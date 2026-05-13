package transcript

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractAssistantTextClaudeMessage(t *testing.T) {
	ev := Event{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "hello"},
				map[string]any{"type": "tool_use", "name": "Bash"},
				map[string]any{"type": "text", "text": "world"},
			},
		},
	}
	got := ExtractAssistantText(ev)
	if len(got) != 2 || got[0] != "hello" || got[1] != "world" {
		t.Fatalf("unexpected text: %#v", got)
	}
}

func TestExtractAssistantTextResult(t *testing.T) {
	ev := Event{"type": "result", "result": "done"}
	got := ExtractAssistantText(ev)
	if len(got) != 1 || got[0] != "done" {
		t.Fatalf("unexpected text: %#v", got)
	}
}

func TestReadLinesSince(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, pos, err := readLinesSince(path, 4)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 8 || len(lines) != 1 || lines[0] != "two" {
		t.Fatalf("lines=%#v pos=%d", lines, pos)
	}
}

func TestEncodeClaudeProjectPath(t *testing.T) {
	got := encodeClaudeProjectPath("/tmp/tmp.ab-CD")
	if got != "-tmp-tmp-ab-CD" {
		t.Fatalf("got %q", got)
	}
}

func TestShouldEmitStreamEventFiltersHooks(t *testing.T) {
	ev := Event{"attachment": map[string]any{"type": "hook_success", "hookName": "SessionStart"}}
	if ShouldEmitStreamEvent(ev, StreamOptions{}) {
		t.Fatal("expected hook event to be filtered")
	}
	if !ShouldEmitStreamEvent(ev, StreamOptions{IncludeHookEvents: true}) {
		t.Fatal("expected hook event when hooks are included")
	}
}

func TestShouldEmitStreamEventFiltersUsers(t *testing.T) {
	ev := Event{"type": "user"}
	if ShouldEmitStreamEvent(ev, StreamOptions{}) {
		t.Fatal("expected user event to be filtered by default")
	}
	if !ShouldEmitStreamEvent(ev, StreamOptions{EmitUserEvents: true}) {
		t.Fatal("expected user event when enabled")
	}
}

func TestShouldEmitStreamEventCompatSkipsTranscriptNoise(t *testing.T) {
	if ShouldEmitStreamEvent(Event{"type": "queue-operation"}, StreamOptions{Compat: true}) {
		t.Fatal("expected queue operation to be skipped in compat stream")
	}
	if !ShouldEmitStreamEvent(Event{"type": "assistant"}, StreamOptions{Compat: true}) {
		t.Fatal("expected assistant in compat stream")
	}
}

func TestWaitTurnExitsOnResultEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	contents := `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}
{"type":"result","result":"done"}
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	// idle=10s ensures we are not relying on the idle heuristic — only the result event
	// should cause an immediate return.
	res, err := WaitTurn(ctx, path, 0, "", time.Now(), 10*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 1*time.Second {
		t.Fatalf("WaitTurn took too long with result event present: %v", time.Since(start))
	}
	if res.Text != "hi\ndone" {
		t.Fatalf("text: %q", res.Text)
	}
	if res.Events < 2 {
		t.Fatalf("events: %d", res.Events)
	}
}

func TestWaitTurnIncrementalReadsAppendedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"first"}]}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		defer f.Close()
		_, _ = f.WriteString(`{"type":"result","result":"second"}` + "\n")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := WaitTurn(ctx, path, 0, "", time.Now(), 10*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "first\nsecond" {
		t.Fatalf("text: %q", res.Text)
	}
}

func TestStreamTurnExitsOnResultEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	contents := `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}
{"type":"result","result":"done"}
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var buf bytes.Buffer
	start := time.Now()
	res, err := StreamTurn(ctx, &buf, path, 0, "", time.Now(), 10*time.Second, "", StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 1*time.Second {
		t.Fatalf("StreamTurn took too long: %v", time.Since(start))
	}
	if res.Events < 2 {
		t.Fatalf("events: %d", res.Events)
	}
	if !strings.Contains(buf.String(), `"result":"done"`) {
		t.Fatalf("expected result event emitted, got: %s", buf.String())
	}
}

func TestReadLinesSinceHoldsPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("one\ntw"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, pos, err := readLinesSince(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 4 || len(lines) != 1 || lines[0] != "one" {
		t.Fatalf("lines=%#v pos=%d", lines, pos)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("o\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	lines, pos, err = readLinesSince(path, pos)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 8 || len(lines) != 1 || lines[0] != "two" {
		t.Fatalf("lines=%#v pos=%d", lines, pos)
	}
}

func TestWaitTurnDoesNotFinishWithPendingToolUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	contents := `{"type":"assistant","message":{"content":[{"type":"text","text":"checking"}],"stop_reason":null}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}],"stop_reason":"tool_use"}}
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(250 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		defer f.Close()
		_, _ = f.WriteString(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}` + "\n")
		_, _ = f.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}}` + "\n")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	res, err := WaitTurn(ctx, path, 0, "", time.Now(), 50*time.Millisecond, "")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 200*time.Millisecond {
		t.Fatalf("WaitTurn returned before pending tool completed")
	}
	if res.Text != "done" {
		t.Fatalf("text: %q", res.Text)
	}
}

func TestShouldEmitStreamEventPartialMessagesFlag(t *testing.T) {
	ev := Event{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "partial"}}, "stop_reason": nil}}
	if ShouldEmitStreamEvent(ev, StreamOptions{}) {
		t.Fatal("expected partial assistant event to be filtered by default")
	}
	if !ShouldEmitStreamEvent(ev, StreamOptions{IncludePartialMessages: true}) {
		t.Fatal("expected partial assistant event when partial messages are included")
	}
}

func TestFindChangedForPromptMatchesUserPrompt(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("AGENTRUN_CLAUDE_DIR", root)
	dir := filepath.Join(root, "projects", encodeClaudeProjectPath(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "old.jsonl")
	newPath := filepath.Join(dir, "new.jsonl")
	if err := os.WriteFile(oldPath, []byte(`{"type":"user","message":{"content":"wrong"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(`{"type":"user","message":{"content":"right"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Second)
	got, err := FindChangedForPrompt(started, cwd, "right")
	if err != nil {
		t.Fatal(err)
	}
	if got != newPath {
		t.Fatalf("got %s want %s", got, newPath)
	}
}
