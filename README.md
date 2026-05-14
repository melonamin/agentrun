# agentrun

`agentrun` runs Claude Code from scripts.

By default it behaves like `claude -p`: one prompt in, output out, then the Claude process exits. Its tmux-backed persistent session mode is opt-in for workflows that need follow-up steering.

Key features:

- drop-in `claude -p` syntax via `agentrun -p`
- stdin prompt support for large orchestrator prompts
- native Claude `text`, `json`, and `stream-json` output
- opt-in persistent sessions with follow-up steering via `--persist-session` and `-s`
- detached/background starts via `-d`
- passthrough of Claude launch flags such as `--model`, `--permission-mode`, and `--allowed-tools`

## Requirements

- Go 1.24+
- `claude` CLI authenticated and available in `PATH`
- `tmux` only for `--persist-session`, `-s`, `--last`, or `-d`

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

Build cross-platform release archives locally:

```bash
just dist
```

## Releases

CI runs on every push and pull request. Pushing a tag that starts with `v` builds Linux and macOS artifacts for amd64/arm64, generates `checksums.txt`, and publishes a GitHub release:

```bash
git tag v0.1.0
git push origin v0.1.0
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

Flags specific to `claude -p` are accepted by `agentrun` and forwarded to Claude:

```text
-p, --print
--output-format <text|json|stream-json>
--input-format <text|stream-json>
--include-partial-messages
--include-hook-events
--replay-user-messages
```

Other Claude flags are passed through to the `claude` command:

```bash
agentrun -p --model sonnet --permission-mode dontAsk --allowed-tools "Read,Bash" "do stuff"
```

Compatibility defaults:

- `agentrun -p "prompt"` defaults to `--output-format text`, matching `claude -p`.
- `agentrun "prompt"` also runs an ephemeral `claude --print` turn.
- tmux-backed persistence requires `--persist-session`, `-s`, `--last`, or `-d`.

### Comparison testing

The repository includes an opt-in parity check against the real `claude -p` command:

```bash
just compare-claude-p
```

It verifies text output, JSON result fields, stream-json output, and stream-json input/replay behavior. This command makes live Claude calls and uses quota.

## Ralphex Provider

`agentrun` can be used as Ralphex's `claude_command` provider. Ralphex passes prompts on stdin, appends `--print`, and normally supplies `--output-format stream-json`; `agentrun` forwards that directly to Claude, so Ralphex receives Claude-compatible stream-json without any Ralphex-specific mode.

Ralphex config locations are `~/.config/ralphex/config` globally or `.ralphex/config` inside a project. The minimal config is:

```ini
claude_command = /absolute/path/to/agentrun
```

That works with Ralphex's default `claude_args`:

```ini
claude_args = --dangerously-skip-permissions --output-format stream-json --verbose
```

On the Ralphex path, `agentrun`:

- reads the full prompt from stdin
- tolerates future/unknown Claude flags instead of failing wrapper startup
- runs Claude as a normal child process instead of moving work into tmux
- forwards Claude stdout/stderr so signal text such as `<<<RALPHEX:ALL_TASKS_DONE>>>` passes through unchanged
- kills the Claude process group on `--turn-timeout`, SIGINT, or SIGTERM

Provider-level compatibility does not implement the Ralphex orchestrator itself: plan execution loops, review phases, dashboards, worktrees, Docker wrappers, and branch management remain Ralphex responsibilities.

## Direct Usage

Run one prompt through `claude --print`:

```bash
agentrun "summarize this repo"
```

Select Claude output formats:

```bash
agentrun --output-format text "summarize this repo"
agentrun --output-format json "summarize this repo"
agentrun --output-format stream-json "summarize this repo"
```

## Persistent Session Usage

Start a new tmux-backed Claude session, send a prompt, wait until the turn appears complete, and print processed JSON parsed from Claude's JSONL transcript:

```bash
agentrun --persist-session "summarize this repo"
```

Follow up in an existing session and stream that turn:

```bash
agentrun -s 1 --stream "now run the tests"
```

`agentrun --persist-session --stream` streams Claude Code's persisted JSONL transcript events, including tool calls, tool results, hooks, attachments, and assistant messages.

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
--persist-session              Start a tmux-backed persistent session
-s, --session <id-or-name>     Send to an existing session
--last                         Use the most recently updated session
-d, --detach                   Send prompt and return immediately
--name <name>                  Name a new agentrun session
--cwd <dir>                    Working directory for a new session
--stream                       Stream raw transcript JSONL in persistent mode
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
```

Other Claude flags are passed through to Claude.

## How it works

By default, `agentrun` starts `claude --print` as a direct child process, forwards stdin/stdout/stderr, and kills Claude's process group on timeout or cancellation.

In persistent mode:

1. `agentrun` starts `claude` inside a detached `tmux` session named `agentrun-<id>-<state-hash>`.
2. Prompts are injected with literal chunked `tmux send-keys`, then Enter.
3. `agentrun` discovers Claude Code JSONL transcripts under `~/.claude/projects/*/*.jsonl`.
4. Output is parsed from JSONL events, not from the `tmux` pane.
5. Turn completion is detected with a conservative idle heuristic: once assistant text appears and the transcript file has stopped changing for `--idle-timeout`.

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
AGENTRUN_CLAUDE_DIR=/path/to/.claude agentrun --persist-session "prompt"
```

## Current MVP limitations

- Busy-session queueing is not implemented yet.
- Interrupt/resume controls are not implemented yet.
- JSONL parsing is schema-tolerant but intentionally minimal.
- Persistent-session turn completion relies on transcript-idle heuristics unless Claude Code exposes stronger lifecycle markers.
- Persistent `--stream` output uses Claude Code's persisted transcript schema.
- Persistent-session partial message chunks are emitted only if the interactive transcript contains them.

## Roadmap

- `tail --follow`
- `--queue`
- `--interrupt`
- richer tool-use summaries
- integration tests with a fake `claude` binary
- configurable Claude/tmux binary paths
