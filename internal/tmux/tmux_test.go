package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
