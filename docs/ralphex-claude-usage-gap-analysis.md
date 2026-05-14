# Ralphex Claude Usage Gap Analysis

This document tracks `agentrun` compatibility with Ralphex's `claude_command`
provider contract.

## Status

Provider-level compatibility is addressed.

`agentrun` is now shaped as a drop-in replacement for the Claude command Ralphex
invokes, not as a replacement for the Ralphex orchestrator itself.

## Addressed Provider Gaps

- Prompts on stdin: addressed. Default print mode forwards stdin directly to
  Claude when no positional prompt is provided.
- `claude -p` behavior: addressed. Default `agentrun` execution runs an
  ephemeral `claude --print` child process.
- Ralphex/Claude flags: addressed. Known print flags are handled and unknown
  Claude-style flags are passed through instead of rejected.
- Stream JSON compatibility: addressed. Drop-in mode forwards Claude's native
  `--output-format stream-json` output unchanged.
- Ralphex signal text: addressed. Drop-in mode does not re-encode or parse the
  stream, and persistent compatibility output disables HTML escaping for signal
  text.
- Session cleanup: addressed. The direct Claude process group is terminated on
  timeout or cancellation.
- Persistent session surprise: addressed. tmux-backed sessions are now opt-in
  through `--persist-session`, `-s`, `--last`, or `-d`.
- Ralphex config path: documented in `README.md` with `claude_command =
  /absolute/path/to/agentrun`.

## Out Of Scope

The following remain Ralphex responsibilities and are not implemented by
`agentrun`:

- plan execution loops
- review orchestration
- external review tool management
- worktree or branch lifecycle
- dashboard output
- Docker wrappers
- Ralphex CLI command compatibility

## Verification

Compatibility was verified with real Claude Code and Ralphex:

- direct `agentrun -> claude` stdin and stream-json run
- direct Ralphex signal preservation probe
- `ralphex -> agentrun -> claude` task execution
- `ralphex -> agentrun -> claude` review mode
- `ralphex -> agentrun -> claude` external review evaluation path
- timeout cleanup smoke proving Claude process group cleanup

Claude Code requires `--verbose` when `--print` is combined with
`--output-format stream-json`. Ralphex's default `claude_args` include
`--verbose`, so this matches the Ralphex provider path.
