#!/usr/bin/env bash
set -euo pipefail

# --help handling
if [[ "${1:-}" == "--help" ]]; then
  cat <<'EOF'
Usage: ${0##*/} [TARGET]
Collect routing report for opencode-go.
If TARGET omitted, defaults to "test:1.1".
Outputs report file path.
EOF
  exit 0
fi

# Helper: run a name and a command safely, capturing output with timestamp, continue on error
run_section() {
  local name="$1"
  shift
  local ts="$(date '+%Y-%m-%d %H:%M:%S')"
  echo "=== [$ts] $name ===" >> "$OUTPUT"
  if "$@" >> "$OUTPUT" 2>&1; then
    echo "=== [$ts] $name DONE ===" >> "$OUTPUT"
  else
    echo "=== [$ts] $name FAILED ===" >> "$OUTPUT"
  fi
  echo >> "$OUTPUT"
}
# Default values
DEFAULT_TARGET="test:1.1"
TS="$(date '+%Y%m%d-%H%M%S')"
TARGET="${1:-$DEFAULT_TARGET}"
OUTPUT="/tmp/opencode/opencode-routing-report-${TS}.txt"
MARKER="ROUTING_MARKER_${TS}_$$"
START_EPOCH=$(date +%s)
ARGS="$*"

# Ensure output directory exists
mkdir -p "$(dirname "$OUTPUT")"

# Section 1: host/time, cwd, script args
run_section "Script metadata" bash -c 'echo "Host: $(hostname)"; echo "Time: $(date)"; echo "CWD: $(pwd)"; echo "Args: $ARGS"'

# Section 2: tmux panes list
run_section "Tmux panes" tmux list-panes -a -F '#{pane_pid} #{session_name}:#{window_index}.#{pane_index} #{pane_current_command} #{pane_current_path}'

# Section 3: target pane capture before
run_section "Target pane (before)" tmux capture-pane -t "${TARGET}" -p -S -120

# Section 4: process tree for target pane pid and opencode processes
run_section "Process tree" bash -c '
  TARGET_PID=$(tmux display-message -p -t "${TARGET}" "#{pane_pid}" 2>/dev/null || true)
  if [[ -n "$TARGET_PID" ]]; then
    pgrep -fa "/opencode|opencode-go|node .*gitnexus|tg-bot-go" || true
    ps -o pid,ppid,cmd -p "$TARGET_PID" --forest || true
  else
    echo "Could not get TARGET_PID"
  fi
'

# Section 5: active TCP connections for ports 4096 and 20128
run_section "TCP connections" ss -tnp "sport = :4096 or sport = :20128" || true

# Section 6: opencode-go health and config (requires curl, jq)
run_section "Opencode health" bash -c "
  curl -s http://127.0.0.1:4096/global/health || echo 'Health endpoint unavailable'
  if command -v jq >/dev/null; then
    curl -s http://127.0.0.1:4096/config/model | jq '.' || true
    curl -s http://127.0.0.1:4096/config/provider | jq '.' || true
    # Deprecated endpoint removed
  fi
"

# Section 7: recent session list (requires jq)
run_section "Recent sessions" bash -c '
  if command -v jq >/dev/null; then
    curl -s http://127.0.0.1:4096/sessions | jq "." || true
  else
    echo "jq not available, skipping recent sessions."
  fi
'

# Section 8: send marker prompt to target pane
run_section "Send marker" tmux send-keys -t "${TARGET}" "${MARKER} reply exactly OK" C-m

# Section 9: sleep
run_section "Sleep" sleep 15

# Section 10: target pane capture after
run_section "Target pane (after)" tmux capture-pane -t "${TARGET}" -p -S -120

# Section 11: opencode-go journal (if systemd unit exists)
run_section "Journal" bash -c "
  if systemctl list-units --type=service | grep -q opencode.service; then
    journalctl -u opencode.service --since "@$START_EPOCH" -n 300 --no-pager || true
  else
    echo 'opencode.service not found.'
  fi
"

# Section 12: 9router docker logs (requires docker)
run_section "9router logs" bash -c '
  if command -v docker >/dev/null && docker ps --filter "name=9router" --format "{{.Names}}" | grep -q .; then
    docker logs --since "$(date --rfc-3339=seconds)" 9router 2>/dev/null | \
      grep -E "POST /v1/chat/completions|ROUTING|No active credentials|model_not_found|gpt-5.5|cx/gpt-5.5|MARKER" || true
  fi
'


# Section 13: full 9router last 200 logs with redaction
run_section "9router recent logs" bash -c '
  if command -v docker >/dev/null && docker ps --filter "name=9router" --format "{{.Names}}" | grep -q .; then
    docker logs --tail 200 9router 2>/dev/null | \
      sed -E "s/(sk-[A-Za-z0-9]+)/[REDACTED]/g; s/(Bearer\\s+[A-Za-z0-9._-]+)/[REDACTED]/g; s/([A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,})/[REDACTED]/g"
  else
    echo '9router container not found.'
  fi
'

# Section 14: target session messages (best effort)
run_section "Target session messages" bash -c '
  # Try to infer session id via tmux pane content (last line) or fallback to latest from opencode API
  SESSION_ID=$(tmux capture-pane -t "${TARGET}" -p -S -1 | grep -Eo "[0-9a-f]{24}" || true)
  if [[ -n "${SESSION_ID}" ]] && command -v jq >/dev/null; then
    curl -s "http://127.0.0.1:4096/session/${SESSION_ID}/messages?limit=20" | jq "." || true
  else
    echo 'Could not determine session id or jq missing.'
  fi
'

# Final marker output
echo "REPORT=${OUTPUT}" >> "$OUTPUT"

echo "Report written to $OUTPUT"
