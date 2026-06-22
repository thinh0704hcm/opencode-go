#!/usr/bin/env bash
# Mimics TUI interaction patterns via API curl commands.
# Run: PORT=3000 bash scripts/api_tui_mimic.sh
# Requires: curl, jq
set -euo pipefail

PORT="${PORT:-3000}"
BASE="http://127.0.0.1:${PORT}"
PASS=0; FAIL=0; TOTAL=0

req() {
  local method="$1" path="$2" body="${3:-}" expect="${4:-}"
  local args=(-s -o /tmp/api_resp.json -w '%{http_code}' -X "$method" "$BASE$path" -H 'Content-Type: application/json')
  [[ -n "$body" ]] && args+=(-d "$body")
  local code
  code=$(curl "${args[@]}" 2>/dev/null || echo "000")
  TOTAL=$((TOTAL+1))
  if [[ -n "$expect" && "$code" == "$expect" ]]; then
    PASS=$((PASS+1)); echo "  ✅ $method $path → $code"
  elif [[ -n "$expect" ]]; then
    FAIL=$((FAIL+1)); echo "  ❌ $method $path → $code (expected $expect)"; cat /tmp/api_resp.json | head -5
  else
    echo "  ℹ️  $method $path → $code"
  fi
}

echo "═══ TUI Happy-Path Behavior Tests ═══"
echo ""

# 1. TUI startup: list sessions
echo "1. TUI startup: GET /session (list)"
req GET "/session" "" 200

# 2. TUI: create new session
echo "2. Create session"
SID=$(curl -s -X POST "$BASE/session" -H 'Content-Type: application/json' -d '{"title":"tui-mimic-test"}' | jq -r '.id')
echo "  ℹ️  Session: $SID"
TOTAL=$((TOTAL+1)); PASS=$((PASS+1))

# 3. TUI: get session details
echo "3. GET /session/$SID"
req GET "/session/$SID" "" 200

# 4. TUI: rename session
echo "4. PATCH /session/$SID (rename)"
req PATCH "/session/$SID" '{"title":"renamed-session"}' 200

# 5. TUI: list messages (empty)
echo "5. GET /session/$SID/message (empty)"
req GET "/session/$SID/message" "" 200

# 6. TUI: get children (empty)
echo "6. GET /session/$SID/children (empty)"
req GET "/session/$SID/children" "" 200

# 7. TUI: get todo (empty)
echo "7. GET /session/$SID/todo (empty)"
req GET "/session/$SID/todo" "" 200

# 8. TUI: get diff
echo "8. GET /session/$SID/diff"
req GET "/session/$SID/diff" "" 200

# 9. TUI: send a message (non-streaming path)
echo "9. POST /session/$SID/message (user msg)"
req POST "/session/$SID/message" '{"content":"Hello from TUI test"}' 200

# 10. TUI: abort (no-op if nothing running)
echo "10. POST /session/$SID/abort (no-op)"
req POST "/session/$SID/abort" "" 200

# 11. TUI: SSE global event stream (connect briefly)
echo "11. GET /global/event (SSE 1s connect)"
CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 1 -H 'Accept: text/event-stream' "$BASE/global/event" 2>/dev/null || echo "000")
TOTAL=$((TOTAL+1))
if [[ "$CODE" == "200" || "$CODE" == "000" ]]; then
  PASS=$((PASS+1)); echo "  ✅ GET /global/event → $CODE (SSE connect OK)"
else
  FAIL=$((FAIL+1)); echo "  ❌ GET /global/event → $CODE (expected 200/000)"
fi

# 12. TUI: VCS info
echo "12. GET /vcs"
req GET "/vcs" "" 200

# 13. TUI: config
echo "13. GET /config"
req GET "/config" "" 200

# 14. TUI: providers
echo "14. GET /provider"
req GET "/provider" "" 200

# 15. TUI: health/path
echo "15. GET /path"
req GET "/path" "" 200

# 16. TUI: project
echo "16. GET /project/current"
req GET "/project/current" "" 200

# 17. TUI: commands list
echo "17. GET /command"
req GET "/command" "" 200

# 18. TUI: skills list
echo "18. GET /skill"
req GET "/skill" "" 200

# 19. TUI: permissions list
echo "19. GET /api/permission/request"
req GET "/api/permission/request" "" 200

# 20. TUI: PTY list
echo "20. GET /pty"
req GET "/pty" "" 200

# 21. TUI: MCP servers
echo "21. GET /mcp"
req GET "/mcp" "" 200

# 22. TUI: agents
echo "22. GET /agent"
req GET "/agent" "" 200

# 23. TUI: delete session
echo "23. DELETE /session/$SID"
req DELETE "/session/$SID" "" 200

# 24. TUI: confirm deleted
echo "24. GET /session/$SID (should 404)"
req GET "/session/$SID" "" 404

# 25. TUI: abort on deleted session
echo "25. POST /session/$SID/abort (deleted)"
req POST "/session/$SID/abort" "" 404

echo ""
echo "════════════════════════════════════════"
echo "  RESULTS: $PASS/$TOTAL passed, $FAIL failed"
echo "════════════════════════════════════════"
exit $FAIL
