package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type inputMessage struct {
	Text string
	Raw  string
}

func stdinIsPipe() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) == 0
}

func readTextPrompt(args []string, r io.Reader) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

func readStreamMessages(r io.Reader) ([]inputMessage, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var out []inputMessage
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		text, ok := extractUserText([]byte(line))
		if !ok {
			continue
		}
		out = append(out, inputMessage{Text: text, Raw: line})
	}
	return out, s.Err()
}

func extractUserText(line []byte) (string, bool) {
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		return "", false
	}
	if typ, _ := ev["type"].(string); typ != "user" {
		return "", false
	}
	if msg, ok := ev["message"].(map[string]any); ok {
		if s, ok := contentText(msg["content"]); ok {
			return s, true
		}
	}
	if s, ok := contentText(ev["content"]); ok {
		return s, true
	}
	return "", false
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

func replayMessage(w io.Writer, raw string) error { _, err := fmt.Fprintln(w, raw); return err }
