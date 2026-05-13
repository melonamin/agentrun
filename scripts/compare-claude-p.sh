#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${AGENTRUN_BIN:-$ROOT/agentrun}"
STATE_DIR="$(mktemp -d)"
WORK_DIR="$(mktemp -d)"
export AGENTRUN_STATE_DIR="$STATE_DIR"
# Historical name is exported too so ad-hoc checks can see the same isolated dir.
export CLAUDP_STATE_DIR="$STATE_DIR"

cleanup() {
  if [[ -f "$STATE_DIR/sessions.json" ]]; then
    jq -r '.sessions[]?.tmux_session // empty' "$STATE_DIR/sessions.json" 2>/dev/null | while read -r tmux_session; do
      [[ -n "$tmux_session" ]] && tmux kill-session -t "$tmux_session" >/dev/null 2>&1 || true
    done
  fi
  rm -rf "$STATE_DIR" "$WORK_DIR"
}
trap cleanup EXIT

need() { command -v "$1" >/dev/null || { echo "missing dependency: $1" >&2; exit 2; }; }
need claude
need tmux
need jq

(cd "$ROOT" && go build -o agentrun ./cmd/agentrun)

capture_agentrun() {
  local name="$1"; shift
  set +e
  (cd "$WORK_DIR" && "$BIN" "$@") >"$WORK_DIR/agentrun-$name.out" 2>"$WORK_DIR/agentrun-$name.err"
  local code=$?
  set -e
  echo "$code" >"$WORK_DIR/agentrun-$name.code"
}

capture_agentrun_stdin() {
  local name="$1" input="$2"; shift 2
  set +e
  printf '%s' "$input" | (cd "$WORK_DIR" && "$BIN" "$@") >"$WORK_DIR/agentrun-$name.out" 2>"$WORK_DIR/agentrun-$name.err"
  local code=${PIPESTATUS[1]}
  set -e
  echo "$code" >"$WORK_DIR/agentrun-$name.code"
}

capture_claude() {
  local name="$1"; shift
  set +e
  (cd "$WORK_DIR" && claude "$@") >"$WORK_DIR/claude-$name.out" 2>"$WORK_DIR/claude-$name.err"
  local code=$?
  set -e
  echo "$code" >"$WORK_DIR/claude-$name.code"
}

capture_claude_stdin() {
  local name="$1" input="$2"; shift 2
  set +e
  printf '%s' "$input" | (cd "$WORK_DIR" && claude "$@") >"$WORK_DIR/claude-$name.out" 2>"$WORK_DIR/claude-$name.err"
  local code=${PIPESTATUS[1]}
  set -e
  echo "$code" >"$WORK_DIR/claude-$name.code"
}

value() { cat "$WORK_DIR/$1.$2"; }

assert_eq() {
  local name="$1" want="$2" got="$3"
  if [[ "$want" != "$got" ]]; then
    echo "FAIL $name: want '$want', got '$got'" >&2
    exit 1
  fi
  echo "ok $name"
}

assert_success_pair() {
  local case_name="$1"
  assert_eq "$case_name exit-code" "$(value claude-$case_name code)" "$(value agentrun-$case_name code)"
  assert_eq "$case_name exit-code-zero" "0" "$(value agentrun-$case_name code)"
  assert_eq "$case_name stderr-agentrun-empty" "" "$(value agentrun-$case_name err)"
  assert_eq "$case_name stderr-claude-empty" "" "$(value claude-$case_name err)"
}

assert_json_file() { jq -e . "$1" >/dev/null; }
assert_jsonl_file() { jq -e . "$1" >/dev/null; }

# 1. Default text output parity: `-p "prompt"` defaults to text.
capture_agentrun text -p --no-session-persistence --turn-timeout 60s 'Reply with exactly: parity-text'
capture_claude text -p 'Reply with exactly: parity-text'
assert_success_pair text
assert_eq text-stdout "$(value claude-text out)" "$(value agentrun-text out)"

# 2. Explicit JSON parity: valid JSON and core result contract fields match.
capture_agentrun json -p --no-session-persistence --output-format json --turn-timeout 60s 'Reply with exactly: parity-json'
capture_claude json -p --output-format json 'Reply with exactly: parity-json'
assert_success_pair json
assert_json_file "$WORK_DIR/agentrun-json.out"
assert_json_file "$WORK_DIR/claude-json.out"
assert_eq json-result "$(jq -r '.result' "$WORK_DIR/claude-json.out")" "$(jq -r '.result' "$WORK_DIR/agentrun-json.out")"
assert_eq json-type "$(jq -r '.type' "$WORK_DIR/claude-json.out")" "$(jq -r '.type' "$WORK_DIR/agentrun-json.out")"
assert_eq json-subtype "$(jq -r '.subtype' "$WORK_DIR/claude-json.out")" "$(jq -r '.subtype' "$WORK_DIR/agentrun-json.out")"
assert_eq json-is-error "$(jq -r '.is_error' "$WORK_DIR/claude-json.out")" "$(jq -r '.is_error' "$WORK_DIR/agentrun-json.out")"

# 3. Stream JSONL parity: valid JSONL, assistant event, final result shape.
capture_agentrun stream -p --no-session-persistence --verbose --output-format stream-json --include-hook-events --turn-timeout 60s 'Reply with exactly: parity-stream'
capture_claude stream -p --verbose --output-format stream-json --include-hook-events 'Reply with exactly: parity-stream'
assert_success_pair stream
assert_jsonl_file "$WORK_DIR/agentrun-stream.out"
assert_jsonl_file "$WORK_DIR/claude-stream.out"
assert_eq stream-result "$(tail -1 "$WORK_DIR/claude-stream.out" | jq -r '.result')" "$(tail -1 "$WORK_DIR/agentrun-stream.out" | jq -r '.result')"
assert_eq stream-final-type "$(tail -1 "$WORK_DIR/claude-stream.out" | jq -r '.type')" "$(tail -1 "$WORK_DIR/agentrun-stream.out" | jq -r '.type')"
assert_eq stream-final-subtype "$(tail -1 "$WORK_DIR/claude-stream.out" | jq -r '.subtype')" "$(tail -1 "$WORK_DIR/agentrun-stream.out" | jq -r '.subtype')"
assert_eq stream-final-is-error "$(tail -1 "$WORK_DIR/claude-stream.out" | jq -r '.is_error')" "$(tail -1 "$WORK_DIR/agentrun-stream.out" | jq -r '.is_error')"
if ! jq -e 'select(.type=="assistant")' "$WORK_DIR/agentrun-stream.out" >/dev/null; then
  echo "FAIL stream-assistant: agentrun stream had no assistant event" >&2
  exit 1
fi
echo "ok stream-assistant"

# 4. stdin text input parity.
capture_agentrun_stdin stdin-text 'Reply with exactly: parity-stdin-text' -p --no-session-persistence --turn-timeout 60s
capture_claude_stdin stdin-text 'Reply with exactly: parity-stdin-text' -p
assert_success_pair stdin-text
assert_eq stdin-text-stdout "$(value claude-stdin-text out)" "$(value agentrun-stdin-text out)"

# 5. stream-json input with replay parity.
stream_input='{"type":"user","message":{"role":"user","content":"Reply with exactly: parity-input"}}
'
capture_agentrun_stdin stream-input "$stream_input" -p --no-session-persistence --input-format stream-json --output-format stream-json --replay-user-messages --turn-timeout 60s
capture_claude_stdin stream-input "$stream_input" -p --verbose --input-format stream-json --output-format stream-json --replay-user-messages
assert_success_pair stream-input
assert_jsonl_file "$WORK_DIR/agentrun-stream-input.out"
assert_jsonl_file "$WORK_DIR/claude-stream-input.out"
assert_eq stream-input-result "$(tail -1 "$WORK_DIR/claude-stream-input.out" | jq -r '.result')" "$(tail -1 "$WORK_DIR/agentrun-stream-input.out" | jq -r '.result')"
assert_eq stream-input-user-count "$(jq -r 'select(.type=="user")|.type' "$WORK_DIR/claude-stream-input.out" | wc -l | tr -d ' ')" "$(jq -r 'select(.type=="user")|.type' "$WORK_DIR/agentrun-stream-input.out" | wc -l | tr -d ' ')"

# 6. Passthrough launch flags parity.
capture_agentrun passthrough -p --no-session-persistence --model sonnet --permission-mode dontAsk --turn-timeout 60s 'Reply with exactly: parity-passthrough'
capture_claude passthrough -p --model sonnet --permission-mode dontAsk 'Reply with exactly: parity-passthrough'
assert_success_pair passthrough
assert_eq passthrough-stdout "$(value claude-passthrough out)" "$(value agentrun-passthrough out)"

# 7. --no-session-persistence cleanup behavior.
capture_agentrun cleanup -p --no-session-persistence --output-format json --turn-timeout 60s 'Reply with exactly: parity-cleanup'
assert_eq cleanup-exit-code-zero "0" "$(value agentrun-cleanup code)"
assert_json_file "$WORK_DIR/agentrun-cleanup.out"
assert_eq cleanup-result "parity-cleanup" "$(jq -r '.result' "$WORK_DIR/agentrun-cleanup.out")"
if [[ -f "$STATE_DIR/sessions.json" ]]; then
  assert_eq cleanup-state-empty "0" "$(jq -r '.sessions | length' "$STATE_DIR/sessions.json")"
else
  echo "ok cleanup-state-empty"
fi
if tmux list-sessions -F '#{session_name}' 2>/dev/null | grep -q '^agentrun-.*'; then
  # Only fail on sessions in this isolated namespace/hash, not on unrelated user sessions.
  hash_suffix="$(jq -r '.agentrun.tmux // empty' "$WORK_DIR/agentrun-cleanup.out" | sed -E 's/^agentrun-[0-9]+-//')"
  if [[ -n "$hash_suffix" ]] && tmux list-sessions -F '#{session_name}' 2>/dev/null | grep -q "^agentrun-[0-9]\+-$hash_suffix$"; then
    echo "FAIL cleanup-tmux: no-session-persistence left tmux session running" >&2
    exit 1
  fi
fi
echo "ok cleanup-tmux"

# 8. Invalid output format behavior: non-zero, empty stdout, invalid-choice stderr.
capture_agentrun invalid-format -p --output-format yaml 'Reply with exactly: never'
capture_claude invalid-format -p --output-format yaml 'Reply with exactly: never'
assert_eq invalid-format-exit-code "$(value claude-invalid-format code)" "$(value agentrun-invalid-format code)"
assert_eq invalid-format-stdout-agentrun-empty "" "$(value agentrun-invalid-format out)"
assert_eq invalid-format-stdout-claude-empty "" "$(value claude-invalid-format out)"
if ! grep -q "invalid" "$WORK_DIR/agentrun-invalid-format.err" || ! grep -q "Allowed choices" "$WORK_DIR/agentrun-invalid-format.err"; then
  echo "FAIL invalid-format-stderr: agentrun did not report invalid allowed choices" >&2
  exit 1
fi
echo "ok invalid-format-stderr"

# 9. Invalid flag behavior: non-zero, empty stdout, unknown option stderr.
capture_agentrun invalid-flag -p --definitely-not-a-real-flag 'Reply with exactly: never'
capture_claude invalid-flag -p --definitely-not-a-real-flag 'Reply with exactly: never'
assert_eq invalid-flag-exit-code "$(value claude-invalid-flag code)" "$(value agentrun-invalid-flag code)"
assert_eq invalid-flag-stdout-agentrun-empty "" "$(value agentrun-invalid-flag out)"
assert_eq invalid-flag-stdout-claude-empty "" "$(value claude-invalid-flag out)"
if ! grep -q "unknown option" "$WORK_DIR/agentrun-invalid-flag.err"; then
  echo "FAIL invalid-flag-stderr: agentrun did not report unknown option" >&2
  exit 1
fi
echo "ok invalid-flag-stderr"

echo "claude -p compatibility comparison passed"
