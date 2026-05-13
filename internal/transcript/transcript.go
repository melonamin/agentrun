package transcript

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Event map[string]any

type Result struct {
	Text       string `json:"text"`
	Events     int    `json:"events"`
	Offset     int64  `json:"offset"`
	Transcript string `json:"transcript"`
}

type StreamOptions struct {
	IncludeHookEvents      bool
	IncludePartialMessages bool
	EmitUserEvents         bool
	Compat                 bool
}

func ClaudeRoot() string {
	if v := os.Getenv("AGENTRUN_CLAUDE_DIR"); v != "" {
		return v
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}

func Candidates(cwd string) ([]string, error) {
	var out []string
	if cwd != "" {
		if abs, err := filepath.Abs(cwd); err == nil {
			// Claude Code stores project transcripts below ~/.claude/projects/<cwd-with-slashes-replaced-by-dashes>.
			// Do not fall back to unrelated project transcripts when cwd is known; other active
			// Claude sessions may be writing at the same time.
			encoded := encodeClaudeProjectPath(abs)
			m, _ := filepath.Glob(filepath.Join(ClaudeRoot(), "projects", encoded, "*.jsonl"))
			out = append(out, m...)
		}
		return out, nil
	}
	for _, pat := range []string{filepath.Join(ClaudeRoot(), "projects", "*", "*.jsonl"), filepath.Join(ClaudeRoot(), "*.jsonl")} {
		m, _ := filepath.Glob(pat)
		out = append(out, m...)
	}
	return out, nil
}

func encodeClaudeProjectPath(path string) string {
	var b strings.Builder
	for _, r := range path {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func FindChanged(after time.Time, cwd string) (string, error) {
	files, _ := Candidates(cwd)
	return newestChanged(files, after)
}

func FindChangedForPrompt(after time.Time, cwd string, prompt string) (string, error) {
	files, _ := Candidates(cwd)
	var matches []string
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || !st.ModTime().After(after.Add(-time.Second)) {
			continue
		}
		if fileContainsUserPrompt(f, prompt) {
			matches = append(matches, f)
		}
	}
	return newestChanged(matches, after)
}

func newestChanged(files []string, after time.Time) (string, error) {
	var best string
	var bt time.Time
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && st.ModTime().After(after.Add(-time.Second)) && st.ModTime().After(bt) {
			best = f
			bt = st.ModTime()
		}
	}
	if best == "" {
		return "", errors.New("no Claude JSONL transcript found; set AGENTRUN_CLAUDE_DIR or try again after Claude starts")
	}
	return best, nil
}

func Size(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func SessionIDFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func PathForSessionID(cwd, sessionID string) string {
	if cwd != "" {
		if abs, err := filepath.Abs(cwd); err == nil {
			return filepath.Join(ClaudeRoot(), "projects", encodeClaudeProjectPath(abs), sessionID+".jsonl")
		}
	}
	return filepath.Join(ClaudeRoot(), sessionID+".jsonl")
}

func WaitTurn(ctx context.Context, transcript string, offset int64, cwd string, started time.Time, idle time.Duration, prompt string) (Result, error) {
	if transcript == "" {
		p, err := locateTranscript(ctx, cwd, started, 30*time.Second, prompt)
		if err != nil {
			return Result{}, err
		}
		transcript = p
	}
	pos := offset
	lastSize := Size(transcript)
	lastChange := time.Now()
	sawResult := false
	sawAssistant := false
	pendingTools := map[string]bool{}
	var parts []string
	events := 0
	for {
		lines, next, err := readLinesSince(transcript, pos)
		if err != nil {
			return Result{}, err
		}
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			events++
			var ev Event
			if json.Unmarshal([]byte(line), &ev) != nil {
				continue
			}
			if typ, _ := ev["type"].(string); typ == "result" {
				sawResult = true
			}
			updatePendingTools(ev, pendingTools)
			texts := extractAssistantText(ev, false)
			if len(texts) > 0 {
				sawAssistant = true
				parts = append(parts, texts...)
			}
		}
		pos = next
		sz := Size(transcript)
		if sz != lastSize {
			lastSize = sz
			lastChange = time.Now()
		}
		if sawResult || (sawAssistant && len(pendingTools) == 0 && time.Since(lastChange) >= idle) {
			return Result{Text: strings.TrimSpace(strings.Join(parts, "\n")), Events: events, Offset: pos, Transcript: transcript}, nil
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func locateTranscript(ctx context.Context, cwd string, started time.Time, timeout time.Duration, prompt string) (string, error) {
	deadline := time.Now().Add(timeout)
	if prompt != "" {
		for time.Now().Before(deadline) {
			p, err := FindChangedForPrompt(started, cwd, prompt)
			if err == nil {
				return p, nil
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	for time.Now().Before(deadline) {
		p, err := FindChanged(started, cwd)
		if err == nil {
			return p, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("timed out locating Claude transcript")
}

func StreamTurn(ctx context.Context, w io.Writer, transcript string, offset int64, cwd string, started time.Time, idle time.Duration, prompt string, opts StreamOptions) (Result, error) {
	if transcript == "" {
		p, err := locateTranscript(ctx, cwd, started, 30*time.Second, prompt)
		if err != nil {
			return Result{}, err
		}
		transcript = p
	}

	if opts.Compat {
		initEvent := map[string]any{
			"type":       "system",
			"subtype":    "init",
			"session_id": SessionIDFromPath(transcript),
		}
		if err := json.NewEncoder(w).Encode(initEvent); err != nil {
			return Result{}, err
		}
	}

	pos := offset
	lastSize := Size(transcript)
	lastChange := time.Now()
	sawAssistant := false
	sawResult := false
	pendingTools := map[string]bool{}
	var parts []string
	events := 0

	for {
		lines, next, err := readLinesSince(transcript, pos)
		if err != nil {
			return Result{}, err
		}
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var ev Event
			parsed := json.Unmarshal([]byte(line), &ev) == nil
			if parsed {
				updatePendingTools(ev, pendingTools)
			}
			if parsed && !ShouldEmitStreamEvent(ev, opts) {
				continue
			}
			events++
			fmt.Fprintln(w, line)
			if parsed {
				if typ, _ := ev["type"].(string); typ == "result" {
					sawResult = true
				}
				texts := extractAssistantText(ev, opts.IncludePartialMessages)
				if len(texts) > 0 {
					sawAssistant = true
					parts = append(parts, texts...)
				}
			}
		}
		pos = next
		sz := Size(transcript)
		if sz != lastSize {
			lastSize = sz
			lastChange = time.Now()
		}
		if sawResult || (sawAssistant && len(pendingTools) == 0 && time.Since(lastChange) >= idle) {
			return Result{Text: strings.TrimSpace(strings.Join(parts, "\n")), Events: events, Offset: pos, Transcript: transcript}, nil
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func ShouldEmitStreamEvent(ev Event, opts StreamOptions) bool {
	if isPartialAssistantEvent(ev) && !opts.IncludePartialMessages {
		return false
	}
	if typ, _ := ev["type"].(string); typ == "user" && !opts.EmitUserEvents {
		return false
	}
	if opts.Compat {
		typ, _ := ev["type"].(string)
		switch typ {
		case "assistant", "result":
			return true
		case "user":
			return opts.EmitUserEvents
		case "system":
			return true
		}
		return opts.IncludeHookEvents && isHookEvent(ev)
	}
	if opts.IncludeHookEvents {
		return true
	}
	return !isHookEvent(ev)
}

func isHookEvent(ev Event) bool {
	attachment, ok := ev["attachment"].(map[string]any)
	if !ok {
		return false
	}
	if _, ok := attachment["hookName"]; ok {
		return true
	}
	if _, ok := attachment["hookEvent"]; ok {
		return true
	}
	if typ, _ := attachment["type"].(string); strings.HasPrefix(typ, "hook_") {
		return true
	}
	return false
}

func readLinesSince(path string, offset int64) ([]string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if offset > 0 {
		_, _ = f.Seek(offset, io.SeekStart)
	}
	reader := bufio.NewReader(f)
	pos := offset
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if errors.Is(err, io.EOF) && !strings.HasSuffix(line, "\n") {
				break
			}
			pos += int64(len(line))
			lines = append(lines, strings.TrimRight(line, "\r\n"))
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, pos, err
		}
	}
	return lines, pos, nil
}

func ParseSince(path string, offset int64) (Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	if offset > 0 {
		_, _ = f.Seek(offset, io.SeekStart)
	}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var parts []string
	events := 0
	for s.Scan() {
		events++
		var ev Event
		if json.Unmarshal(s.Bytes(), &ev) == nil {
			parts = append(parts, ExtractAssistantText(ev)...)
		}
	}
	st, _ := f.Stat()
	return Result{Text: strings.TrimSpace(strings.Join(parts, "\n")), Events: events, Offset: st.Size(), Transcript: path}, s.Err()
}

func ExtractAssistantText(ev Event) []string {
	return extractAssistantText(ev, true)
}

func extractAssistantText(ev Event, includePartial bool) []string {
	typ, _ := ev["type"].(string)
	if typ != "assistant" && typ != "result" {
		return nil
	}
	if typ == "assistant" && !includePartial && isPartialAssistantEvent(ev) {
		return nil
	}
	if typ == "result" {
		if s, ok := ev["result"].(string); ok && s != "" {
			return []string{s}
		}
	}
	msg, _ := ev["message"].(map[string]any)
	content, ok := msg["content"].([]any)
	if !ok {
		content, _ = ev["content"].([]any)
	}
	var out []string
	for _, c := range content {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] == "text" {
			if t, ok := m["text"].(string); ok && t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

func isPartialAssistantEvent(ev Event) bool {
	if typ, _ := ev["type"].(string); typ != "assistant" {
		return false
	}
	msg, ok := ev["message"].(map[string]any)
	if !ok {
		return false
	}
	_, hasStopReason := msg["stop_reason"]
	if !hasStopReason {
		return false
	}
	return msg["stop_reason"] == nil
}

func updatePendingTools(ev Event, pending map[string]bool) {
	typ, _ := ev["type"].(string)
	for _, item := range eventContent(ev) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := m["type"].(string)
		switch itemType {
		case "tool_use":
			if typ == "assistant" {
				if id, _ := m["id"].(string); id != "" {
					pending[id] = true
				}
			}
		case "tool_result":
			if id, _ := m["tool_use_id"].(string); id != "" {
				delete(pending, id)
			}
		}
	}
}

func fileContainsUserPrompt(path, prompt string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for s.Scan() {
		var ev Event
		if json.Unmarshal(s.Bytes(), &ev) != nil {
			continue
		}
		if text, ok := UserText(ev); ok && text == prompt {
			return true
		}
	}
	return false
}

func UserText(ev Event) (string, bool) {
	if typ, _ := ev["type"].(string); typ != "user" {
		return "", false
	}
	if msg, ok := ev["message"].(map[string]any); ok {
		if s, ok := contentText(msg["content"]); ok {
			return s, true
		}
	}
	return contentText(ev["content"])
}

func eventContent(ev Event) []any {
	if msg, ok := ev["message"].(map[string]any); ok {
		if content, ok := msg["content"].([]any); ok {
			return content
		}
	}
	if content, ok := ev["content"].([]any); ok {
		return content
	}
	return nil
}

func contentText(v any) (string, bool) {
	switch c := v.(type) {
	case string:
		return c, c != ""
	case []any:
		var parts []string
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] == "text" {
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n"), len(parts) > 0
	default:
		return "", false
	}
}
