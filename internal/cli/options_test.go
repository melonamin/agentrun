package cli

import "testing"

func TestParseNativeDefaultsToJSON(t *testing.T) {
	opts, err := parsePromptArgs([]string{"hello"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if opts.PrintCompat || opts.OutputFormat != formatJSON || opts.InputFormat != inputText || opts.PromptArgs[0] != "hello" {
		t.Fatalf("unexpected opts: %#v", opts)
	}
}

func TestParsePrintDefaultsToText(t *testing.T) {
	opts, err := parsePromptArgs([]string{"-p", "hello"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !opts.PrintCompat || opts.OutputFormat != formatText {
		t.Fatalf("unexpected opts: %#v", opts)
	}
}

func TestParsePrintJSON(t *testing.T) {
	opts, err := parsePromptArgs([]string{"-p", "--output-format", "json", "hello"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutputFormat != formatJSON {
		t.Fatalf("unexpected output: %s", opts.OutputFormat)
	}
}

func TestParseStreamAlias(t *testing.T) {
	opts, err := parsePromptArgs([]string{"--stream", "hello"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutputFormat != formatStreamJSON {
		t.Fatalf("unexpected output: %s", opts.OutputFormat)
	}
}

func TestParseRejectsInvalidOutput(t *testing.T) {
	_, err := parsePromptArgs([]string{"-p", "--output-format", "yaml", "hello"}, "/repo")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseCollectsPassthrough(t *testing.T) {
	opts, err := parsePromptArgs([]string{"-p", "--model", "sonnet", "--permission-mode=dontAsk", "--output-format", "stream-json", "--include-hook-events", "hello"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "sonnet", "--permission-mode=dontAsk"}
	if len(opts.ClaudeArgs) != len(want) {
		t.Fatalf("claude args %#v", opts.ClaudeArgs)
	}
	for i := range want {
		if opts.ClaudeArgs[i] != want[i] {
			t.Fatalf("claude args %#v", opts.ClaudeArgs)
		}
	}
	if !opts.IncludeHookEvents || opts.OutputFormat != formatStreamJSON {
		t.Fatalf("unexpected opts %#v", opts)
	}
}

func TestParseExistingSessionWithPassthroughIsRepresentable(t *testing.T) {
	opts, err := parsePromptArgs([]string{"-s", "1", "--model", "sonnet", "hello"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if opts.SessionID != "1" || len(opts.ClaudeArgs) == 0 {
		t.Fatalf("unexpected opts %#v", opts)
	}
}

func TestParseOptionalClaudeFlagSeparateValue(t *testing.T) {
	opts, err := parsePromptArgs([]string{"-p", "--resume", "session-123", "continue"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	wantClaude := []string{"--resume", "session-123"}
	if len(opts.ClaudeArgs) != len(wantClaude) {
		t.Fatalf("claude args %#v", opts.ClaudeArgs)
	}
	for i := range wantClaude {
		if opts.ClaudeArgs[i] != wantClaude[i] {
			t.Fatalf("claude args %#v", opts.ClaudeArgs)
		}
	}
	if len(opts.PromptArgs) != 1 || opts.PromptArgs[0] != "continue" {
		t.Fatalf("prompt args %#v", opts.PromptArgs)
	}
}

func TestParseOptionalClaudeFlagWithoutValueBeforeFlag(t *testing.T) {
	opts, err := parsePromptArgs([]string{"-p", "--resume", "--model", "sonnet", "continue"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	wantClaude := []string{"--resume", "--model", "sonnet"}
	if len(opts.ClaudeArgs) != len(wantClaude) {
		t.Fatalf("claude args %#v", opts.ClaudeArgs)
	}
	for i := range wantClaude {
		if opts.ClaudeArgs[i] != wantClaude[i] {
			t.Fatalf("claude args %#v", opts.ClaudeArgs)
		}
	}
	if len(opts.PromptArgs) != 1 || opts.PromptArgs[0] != "continue" {
		t.Fatalf("prompt args %#v", opts.PromptArgs)
	}
}
