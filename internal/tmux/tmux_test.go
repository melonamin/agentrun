package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSendTextTerminatesTmuxOptionsBeforeLiteralText(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	binPath := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nprintf 'CALL\\n' >> \"$TMUX_LOG\"\nprintf '%s\\n' \"$@\" >> \"$TMUX_LOG\"\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("TMUX_LOG", logPath)

	client := Client{Bin: binPath}
	if err := client.SendText("session-name", "- Read the plan\nplain text"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "send-keys\n-t\nsession-name\n-l\n--\n- Read the plan\n") {
		t.Fatalf("literal text starting with dash was not protected with --:\n%s", got)
	}
	if !strings.Contains(got, "send-keys\n-t\nsession-name\n-l\n--\nplain text\n") {
		t.Fatalf("literal text was not sent after --:\n%s", got)
	}
}

func TestSendTextChunksLongLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	binPath := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nprintf 'CALL\\n' >> \"$TMUX_LOG\"\nprintf '%s\\n' \"$@\" >> \"$TMUX_LOG\"\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("TMUX_LOG", logPath)

	client := Client{Bin: binPath}
	if err := client.SendText("session-name", strings.Repeat("x", sendKeysChunkSize*2+17)); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	got := string(data)
	if calls := strings.Count(got, "CALL\n"); calls != 4 {
		t.Fatalf("send-keys calls=%d, want 4:\n%s", calls, got[:200])
	}
	if !strings.HasSuffix(got, "Enter\n") {
		t.Fatalf("expected final Enter:\n%s", got)
	}
}

func TestChunkStringPreservesUTF8(t *testing.T) {
	chunks := chunkString("åååå", 3)
	if strings.Join(chunks, "") != "åååå" {
		t.Fatalf("chunks did not reassemble: %#v", chunks)
	}
	for _, chunk := range chunks {
		if !utf8.ValidString(chunk) {
			t.Fatalf("invalid utf8 chunk: %#v", chunk)
		}
	}
}

func TestNewSessionLaunchesClaudeWithArgs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	binPath := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TMUX_LOG\"\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("TMUX_LOG", logPath)

	client := Client{Bin: binPath}
	if err := client.NewSession("s", "/repo", []string{"--model", "sonnet"}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	got := string(data)
	for _, want := range []string{"new-session\n", "-s\ns\n", "-c\n/repo\n", "claude\n", "--model\nsonnet\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
