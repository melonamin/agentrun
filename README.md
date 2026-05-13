# agentrun

`agentrun` runs AI coding-agent sessions from scripts while keeping them steerable.

Today it targets Claude Code: it can be used as a drop-in `claude -p` replacement, but unlike `claude -p` it keeps the underlying interactive session alive so you can send follow-up messages later.

Key features:

- drop-in `claude -p` syntax via `agentrun -p`
- JSON-first native output for scripts
- raw JSONL streaming from Claude Code transcripts
- persistent sessions with follow-up steering via `-s`
- detached/background starts via `-d`
- passthrough of Claude launch flags such as `--model`, `--permission-mode`, and `--allowed-tools`

## Requirements

- Go 1.24+
- `tmux`
- `claude` CLI authenticated and available in `PATH`

## Install

```bash
go install github.com/melonamin/agentrun/cmd/agentrun@latest
```

For local development:

```bash
go build -o agentrun ./cmd/agentrun
```

Run local checks:

```bash
just check
```

Run opt-in comparison checks against real `claude -p` behavior. This uses Claude quota:

```bash
just compare-claude-p
```

## Drop-in `claude -p` compatibility

For the easiest migration, replace only the binary name:

```diff
- claude -p "summarize this repo"
+ agentrun -p "summarize this repo"
```

`agentrun -p` accepts the print-mode flags from Claude Code:

```bash
agentrun -p --output-format text "do stuff"
agentrun -p --output-format json "do stuff"
agentrun -p --output-format stream-json --include-partial-messages --include-hook-events "do stuff"
agentrun -p --input-format stream-json --output-format stream-json < input.jsonl
```

Flags specific to `claude -p` are implemented by `agentrun`:

```text
-p, --print
--output-format <text|json|stream-json>
--input-format <text|stream-json>
--include-partial-messages
--include-hook-events
--replay-user-messages
--no-session-persistence
```

Other Claude flags are passed to the interactive `claude` command when a new tmux session is launched:

```bash
agentrun -p --model sonnet --permission-mode dontAsk --allowed-tools "Read,Bash" "do stuff"
```

Passthrough launch flags only apply to new sessions. They are rejected when targeting an existing session with `-s`.

Compatibility defaults:

- `agentrun -p "prompt"` defaults to `--output-format text`, matching `claude -p`.
- `agentrun "prompt"` defaults to JSON, the native `agentrun` behavior.
- `--no-session-persistence` removes the tmux-backed session after the turn.

### Comparison testing

The repository includes an opt-in parity check against the real `claude -p` command:

```bash
just compare-claude-p
```

It verifies text output, JSON result fields, stream-json output, and stream-json input/replay behavior. This command makes live Claude calls and uses quota.

## Native `agentrun` usage

Start a new Claude session, send a prompt, wait until the turn appears complete, and print processed JSON parsed from Claude's JSONL transcript:

```bash
agentrun "summarize this repo"
```

Default output is JSON:

```json
{
  "session": "1",
  "status": "idle",
  "tmux": "agentrun-1-a1b2c3d4",
  "cwd": "/path/to/repo",
  "transcript": "/home/me/.claude/projects/-path-to-repo/session.jsonl",
  "result": {
    "text": "...",
    "events": 5
  }
}
```

Human-readable text output:

```bash
agentrun --text "summarize this repo"
```

Stream raw Claude transcript JSONL events as they are written, including tool calls, tool results, hooks, attachments, and assistant messages:

```bash
agentrun --stream "do stuff"
```

Follow up in an existing session and stream that turn:

```bash
agentrun -s 1 --stream "now run the tests"
```

The drop-in `claude -p` syntax for the same stream is:

```bash
agentrun -p \
  --output-format stream-json \
  --include-partial-messages \
  --include-hook-events \
  "do stuff"
```

`agentrun --stream` / `agentrun -p --output-format stream-json` do not use `claude -p`; they drive the interactive Claude session in `tmux` and stream from Claude Code's persisted JSONL transcript.

Start in detached mode and return immediately:

```bash
agentrun -d "refactor the parser"
```

Send a follow-up to an existing session:

```bash
agentrun -s 1 "also add tests"
```

Use the most recently updated session:

```bash
agentrun --last "continue"
```

Assistant text only:

```bash
agentrun -q "write a changelog entry"
```

## Session commands

```bash
agentrun list
agentrun status -s 1
agentrun attach -s 1
agentrun transcript -s 1
agentrun kill -s 1
```

`list` and `status` also default to JSON. Use `--text` for tabular/human output:

```bash
agentrun list --text
agentrun status -s 1 --text
```

## Flags

Native `agentrun` flags:

```text
-s, --session <id-or-name>     Send to an existing session
--last                         Use the most recently updated session
-d, --detach                   Send prompt and return immediately
--name <name>                  Name a new agentrun session
--cwd <dir>                    Working directory for a new session
--stream                       Alias for stream-json output in native mode
--text                         Human-readable text output
-q, --quiet                    Assistant text only
--idle-timeout <duration>      Transcript stability duration for turn completion
--turn-timeout <duration>      Maximum wait for a turn
```

`claude -p` compatibility flags implemented by `agentrun`:

```text
-p, --print
--output-format <text|json|stream-json>
--input-format <text|stream-json>
--include-partial-messages
--include-hook-events
--replay-user-messages
--no-session-persistence
```

Other Claude flags are collected and passed through when creating a new session.

## How it works

1. `agentrun` starts `claude` inside a detached `tmux` session named `agentrun-<id>-<state-hash>`.
2. Prompts are injected with literal `tmux send-keys`, then Enter.
3. `agentrun` discovers Claude Code JSONL transcripts under `~/.claude/projects/*/*.jsonl`.
4. Output is parsed from JSONL events, not from the `tmux` pane.
5. Turn completion is detected with a conservative idle heuristic: once assistant text appears and the transcript file has stopped changing for `--idle-timeout`.

`agentrun` intentionally does not call `claude -p`; print compatibility is implemented by controlling an interactive `claude` process and reading its persisted transcript.

## State

Session registry defaults to:

```text
$XDG_STATE_HOME/agentrun/sessions.json
```

or:

```text
~/.local/state/agentrun/sessions.json
```

Override with:

```bash
AGENTRUN_STATE_DIR=/tmp/agentrun agentrun list
```

Override Claude transcript root with:

```bash
AGENTRUN_CLAUDE_DIR=/path/to/.claude agentrun "prompt"
```

## Current MVP limitations

- Busy-session queueing is not implemented yet.
- Interrupt/resume controls are not implemented yet.
- JSONL parsing is schema-tolerant but intentionally minimal.
- Turn completion relies on transcript-idle heuristics unless Claude Code exposes stronger lifecycle markers.
- Stream output uses Claude Code's persisted transcript schema, which is not byte-for-byte identical to `claude -p --output-format stream-json`.
- Partial message chunks are emitted only if the interactive transcript contains them.

## Roadmap

- `tail --follow`
- `--queue`
- `--interrupt`
- richer tool-use summaries
- integration tests with a fake `claude` binary
- configurable Claude/tmux binary paths
