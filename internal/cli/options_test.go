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

func TestParsePrintStreamDoesNotUseTmuxByDefault(t *testing.T) {
	opts, err := parsePromptArgs([]string{"--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose", "--print"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if opts.UsesTmuxSession() || !opts.PrintCompat || opts.OutputFormat != formatStreamJSON || !opts.OutputFormatExplicit {
		t.Fatalf("unexpected opts: %#v", opts)
	}
}

func TestParsePersistSessionOptIn(t *testing.T) {
	opts, err := parsePromptArgs([]string{"--persist-session", "-p", "--output-format", "stream-json"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !opts.UsesTmuxSession() || !opts.PersistSession || !opts.PrintCompat || opts.OutputFormat != formatStreamJSON {
		t.Fatalf("unexpected opts: %#v", opts)
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

func TestParseUnknownClaudeFlagIsTolerated(t *testing.T) {
	opts, err := parsePromptArgs([]string{"-p", "--future-claude-flag", "--output-format", "stream-json"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.ClaudeArgs) != 1 || opts.ClaudeArgs[0] != "--future-claude-flag" {
		t.Fatalf("claude args %#v", opts.ClaudeArgs)
	}
}

func TestDirectClaudeArgsDefaultsToPrintWithoutOutputFormat(t *testing.T) {
	opts, err := parsePromptArgs([]string{"hello"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	got := directClaudeArgs(opts)
	want := []string{"--print", "hello"}
	if len(got) != len(want) {
		t.Fatalf("args %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args %#v", got)
		}
	}
}

func TestDirectClaudeArgsPreservesRalphexShape(t *testing.T) {
	opts, err := parsePromptArgs([]string{"--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose", "--print"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	got := directClaudeArgs(opts)
	want := []string{"--dangerously-skip-permissions", "--verbose", "--print", "--output-format", "stream-json"}
	if len(got) != len(want) {
		t.Fatalf("args %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args %#v", got)
		}
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
