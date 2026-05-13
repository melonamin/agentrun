package cli

import (
	"fmt"
	"strings"
	"time"
)

const (
	formatText       = "text"
	formatJSON       = "json"
	formatStreamJSON = "stream-json"
	inputText        = "text"
	inputStreamJSON  = "stream-json"
)

type promptOptions struct {
	SessionID              string
	Name                   string
	CWD                    string
	Last                   bool
	Detach                 bool
	PrintCompat            bool
	Quiet                  bool
	NoSessionPersistence   bool
	IncludePartialMessages bool
	IncludeHookEvents      bool
	ReplayUserMessages     bool
	OutputFormat           string
	InputFormat            string
	IdleTimeout            time.Duration
	TurnTimeout            time.Duration
	ClaudeArgs             []string
	PromptArgs             []string
}

type cliError struct {
	Message     string
	ClaudeStyle bool
}

func (e cliError) Error() string { return e.Message }

func claudeStyleError(message string) error {
	return cliError{Message: message, ClaudeStyle: true}
}

func defaultPromptOptions(cwd string) promptOptions {
	return promptOptions{CWD: cwd, OutputFormat: formatJSON, InputFormat: inputText, IdleTimeout: 2 * time.Second, TurnTimeout: 30 * time.Minute}
}

func parsePromptArgs(argv []string, cwd string) (promptOptions, error) {
	opts := defaultPromptOptions(cwd)
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			opts.PromptArgs = append(opts.PromptArgs, argv[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			opts.PromptArgs = append(opts.PromptArgs, argv[i:]...)
			break
		}
		name, val, hasVal := splitFlag(a)
		switch name {
		case "-p", "--print":
			opts.PrintCompat = true
		case "-s", "--session":
			v, ni, err := flagValue(name, val, hasVal, argv, i)
			if err != nil {
				return opts, err
			}
			opts.SessionID = v
			i = ni
		case "--last":
			opts.Last = true
		case "--cwd":
			v, ni, err := flagValue(name, val, hasVal, argv, i)
			if err != nil {
				return opts, err
			}
			opts.CWD = v
			i = ni
		case "--name":
			v, ni, err := flagValue(name, val, hasVal, argv, i)
			if err != nil {
				return opts, err
			}
			opts.Name = v
			i = ni
		case "-d", "--detach":
			opts.Detach = true
		case "--stream":
			opts.OutputFormat = formatStreamJSON
		case "--text":
			opts.OutputFormat = formatText
		case "-q", "--quiet":
			opts.Quiet = true
			opts.OutputFormat = formatText
		case "--json":
			opts.OutputFormat = formatJSON
		case "--idle-timeout":
			v, ni, err := flagValue(name, val, hasVal, argv, i)
			if err != nil {
				return opts, err
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				return opts, fmt.Errorf("invalid --idle-timeout: %w", err)
			}
			opts.IdleTimeout = d
			i = ni
		case "--turn-timeout":
			v, ni, err := flagValue(name, val, hasVal, argv, i)
			if err != nil {
				return opts, err
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				return opts, fmt.Errorf("invalid --turn-timeout: %w", err)
			}
			opts.TurnTimeout = d
			i = ni
		case "--output-format":
			v, ni, err := flagValue(name, val, hasVal, argv, i)
			if err != nil {
				return opts, err
			}
			opts.OutputFormat = v
			i = ni
		case "--input-format":
			v, ni, err := flagValue(name, val, hasVal, argv, i)
			if err != nil {
				return opts, err
			}
			opts.InputFormat = v
			i = ni
		case "--include-partial-messages":
			opts.IncludePartialMessages = true
		case "--include-hook-events":
			opts.IncludeHookEvents = true
		case "--replay-user-messages":
			opts.ReplayUserMessages = true
		case "--no-session-persistence":
			opts.NoSessionPersistence = true
		default:
			consumed, ni, err := collectClaudeArg(name, val, hasVal, argv, i)
			if err != nil {
				return opts, err
			}
			opts.ClaudeArgs = append(opts.ClaudeArgs, consumed...)
			i = ni
		}
	}
	if opts.PrintCompat && opts.OutputFormat == formatJSON && !containsOutputFlag(argv) && !containsNativeJSONFlag(argv) {
		opts.OutputFormat = formatText
	}
	if err := validateFormats(opts); err != nil {
		return opts, err
	}
	return opts, nil
}

func validateFormats(opts promptOptions) error {
	switch opts.OutputFormat {
	case formatText, formatJSON, formatStreamJSON:
	default:
		return claudeStyleError(fmt.Sprintf("error: option '--output-format <format>' argument '%s' is invalid. Allowed choices are text, json, stream-json.", opts.OutputFormat))
	}
	switch opts.InputFormat {
	case inputText, inputStreamJSON:
	default:
		return claudeStyleError(fmt.Sprintf("error: option '--input-format <format>' argument '%s' is invalid. Allowed choices are text, stream-json.", opts.InputFormat))
	}
	return nil
}

func splitFlag(arg string) (name, val string, hasVal bool) {
	if i := strings.IndexByte(arg, '='); i >= 0 {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

func flagValue(name, val string, hasVal bool, argv []string, i int) (string, int, error) {
	if hasVal {
		return val, i, nil
	}
	if i+1 >= len(argv) {
		return "", i, fmt.Errorf("%s requires a value", name)
	}
	return argv[i+1], i + 1, nil
}

func containsOutputFlag(argv []string) bool {
	for _, a := range argv {
		if a == "--output-format" || strings.HasPrefix(a, "--output-format=") || a == "--stream" || a == "--text" || a == "-q" || a == "--quiet" {
			return true
		}
	}
	return false
}
func containsNativeJSONFlag(argv []string) bool {
	for _, a := range argv {
		if a == "--json" {
			return true
		}
	}
	return false
}

var claudeBoolFlags = map[string]bool{
	"--allow-dangerously-skip-permissions": true, "--bare": true, "--brief": true, "--chrome": true, "-c": true, "--continue": true,
	"--dangerously-skip-permissions": true, "--disable-slash-commands": true, "--exclude-dynamic-system-prompt-sections": true,
	"--fork-session": true, "-h": true, "--help": true, "--ide": true, "--mcp-debug": true, "--no-chrome": true,
	"--strict-mcp-config": true, "--verbose": true, "-v": true, "--version": true,
}

var claudeValueFlags = map[string]bool{
	"--add-dir": true, "--agent": true, "--agents": true, "--allowedTools": true, "--allowed-tools": true, "--append-system-prompt": true,
	"--betas": true, "--debug-file": true, "--disallowedTools": true, "--disallowed-tools": true, "--effort": true, "--file": true,
	"--json-schema": true, "--mcp-config": true, "--model": true, "--permission-mode": true, "--plugin-dir": true, "--plugin-url": true,
	"--remote-control-session-name-prefix": true, "--session-id": true, "--setting-sources": true, "--settings": true, "--system-prompt": true,
	"--tools": true,
}

var claudeOptionalValueFlags = map[string]bool{"--debug": true, "--from-pr": true, "--remote-control": true, "-r": true, "--resume": true, "-w": true, "--worktree": true, "--tmux": true}

func collectClaudeArg(name, val string, hasVal bool, argv []string, i int) ([]string, int, error) {
	if hasVal {
		return []string{argv[i]}, i, nil
	}
	if claudeBoolFlags[name] {
		return []string{name}, i, nil
	}
	if claudeValueFlags[name] {
		v, ni, err := flagValue(name, val, hasVal, argv, i)
		if err != nil {
			return nil, i, err
		}
		return []string{name, v}, ni, nil
	}
	if claudeOptionalValueFlags[name] {
		if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
			return []string{name, argv[i+1]}, i + 1, nil
		}
		return []string{name}, i, nil
	}
	return nil, i, claudeStyleError(fmt.Sprintf("error: unknown option '%s'", name))
}
