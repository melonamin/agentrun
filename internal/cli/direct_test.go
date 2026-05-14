package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunDirectPrintExecutesClaudeChildWithStdin(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(binDir, "claude")
	script := `#!/bin/sh
printf 'PWD=%s\n' "$PWD"
printf 'ARGS=%s\n' "$*"
input="$(cat)"
printf 'STDIN=%s\n' "$input"
`
	if err := os.WriteFile(claudePath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldStdout := os.Stdout
	oldStdin := os.Stdin
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutW
	os.Stdin = stdinR
	defer func() {
		os.Stdout = oldStdout
		os.Stdin = oldStdin
	}()
	if _, err := stdinW.WriteString("hello from stdin"); err != nil {
		t.Fatal(err)
	}
	_ = stdinW.Close()

	opts := promptOptions{
		CWD:                  workDir,
		OutputFormat:         formatStreamJSON,
		OutputFormatExplicit: true,
		InputFormat:          inputText,
		TurnTimeout:          2 * time.Second,
		ClaudeArgs:           []string{"--model", "sonnet"},
	}
	if err := runDirectPrint(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	_ = stdoutW.Close()
	data, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		"PWD=" + workDir,
		"ARGS=--model sonnet --print --output-format stream-json",
		"STDIN=hello from stdin",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
