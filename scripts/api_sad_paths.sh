#!/usr/bin/env bash
# API sad-path test suite for opencode-go server.
# Run: PORT=3000 bash scripts/api_sad_paths.sh
# Requires: curl, jq
set -euo pipefail

PORT="${PORT:-3000}"
BASE="http://127.0.0.1:${PORT}"
PASS=0; FAIL=0; TOTAL=0

# --- helpers ---
req() {
  # req METHOD PATH [BODY] [EXPECTED_CODE]
  local method="$1" path="$2" body="${3:-}" expect="${4:-}"
  local args=(-s -o /tmp/api_resp.json -w '%{http_code}' -X "$method" "$BASE$path" -H 'Content-Type: application/json')
  [[ -n "$body" ]] && args+=(-d "$body")
  local code
  code=$(curl "${args[@]}" 2>/dev/null || echo "000")
  TOTAL=$((TOTAL+1))
  if [[ -n "$expect" && "$code" == "$expect" ]]; then
    PASS=$((PASS+1)); echo "  ✅ $method $path → $code (expected $expect)"
  elif [[ -n "$expect" ]]; then
    FAIL=$((FAIL+1)); echo "  ❌ $method $path → $code (expected $expect)"; cat /tmp/api_resp.json | head -5
  else
    echo "  ℹ️  $method $path → $code"
  fi
  echo "$code"
}

# ============================================================
# 1. SESSION LIFECYCLE SAD PATHS
# ============================================================
echo ""
echo "═══ 1. Session Lifecycle Sad Paths ═══"

echo "1.1 Non-existent session GET"
req GET "/session/ses_nonexistent000000000" "" 404

echo "1.2 Non-existent session PATCH"
req PATCH "/session/ses_nonexistent000000000" '{"title":"x"}' 404

echo "1.3 Non-existent session DELETE"
req DELETE "/session/ses_nonexistent000000000" "" 404

echo "1.4 Non-existent session GET messages"
req GET "/session/ses_nonexistent000000000/message" "" 404

echo "1.5 Non-existent session POST message"
req POST "/session/ses_nonexistent000000000/message" '{"content":"hi"}' 404

echo "1.6 Non-existent session abort"
req POST "/session/ses_nonexistent000000000/abort" "" 404

echo "1.7 Non-existent session summarize"
req POST "/session/ses_nonexistent000000000/summarize" "" 404

echo "1.8 Non-existent session fork"
req POST "/session/ses_nonexistent000000000/fork" '{}' 404

echo "1.9 Non-existent session share"
req POST "/session/ses_nonexistent000000000/share" '{}' 404

echo "1.10 Non-existent session shell"
req POST "/session/ses_nonexistent000000000/shell" '{"command":"echo hi"}' 404

echo "1.11 Non-existent session command"
req POST "/session/ses_nonexistent000000000/command" '{"command":"help","arguments":"test"}' 404

echo "1.12 Non-existent session children"
req GET "/session/ses_nonexistent000000000/children" "" 404

echo "1.13 Non-existent session todo"
req GET "/session/ses_nonexistent000000000/todo" "" 404

echo "1.14 Non-existent session diff"
req GET "/session/ses_nonexistent000000000/diff" "" 404

echo "1.15 Non-existent session revert"
req POST "/session/ses_nonexistent000000000/revert" '{}' 404

echo "1.16 Non-existent session unrevert"
req POST "/session/ses_nonexistent000000000/unrevert" '{}' 404

echo "1.17 Non-existent session init"
req POST "/session/ses_nonexistent000000000/init" '{"messageID":"msg_123","modelID":"test","providerID":"test"}' 404

# ============================================================
# 2. SESSION CREATE SAD PATHS
# ============================================================
echo ""
echo "═══ 2. Session Create Sad Paths ═══"

echo "2.1 Create with empty body"
req POST "/session" '{}' ""

echo "2.2 Create with missing title field"
req POST "/session" '{"notTitle":"x"}' ""

echo "2.3 Create with null title"
req POST "/session" '{"title":null}' ""

echo "2.4 Create with empty string title"
req POST "/session" '{"title":""}' ""

echo "2.5 Create with numeric title (wrong type)"
req POST "/session" '{"title":12345}' ""

echo "2.6 Create with oversized title (10KB)"
LONG_TITLE=$(python3 -c "print('A'*10000)")
req POST "/session" "{\"title\":\"$LONG_TITLE\"}" ""

# ============================================================
# 3. MESSAGE SENDING SAD PATHS
# ============================================================
echo ""
echo "═══ 3. Message Sending Sad Paths ═══"

echo "3.1 Post message with invalid JSON body"
req POST "/session/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/message" "NOT_JSON" 404

# Create a real session for further tests
echo "3.2 Create test session for message tests"
SESSION_RESP=$(curl -s -X POST "$BASE/session" -H 'Content-Type: application/json' -d '{"title":"sad-path-test"}' 2>/dev/null)
SID=$(echo "$SESSION_RESP" | jq -r '.id // empty' 2>/dev/null || true)
if [[ -z "$SID" ]]; then
  echo "  ⚠️  Could not create test session; skipping message tests"
  SID="SKIP"
else
  echo "  ℹ️  Created session: $SID"
fi

if [[ "$SID" != "SKIP" ]]; then
  echo "3.3 Post message with empty body"
  req POST "/session/$SID/message" "" 400

  echo "3.4 Post message with null content"
  req POST "/session/$SID/message" '{"content":null}' 400

  echo "3.5 Post message with empty string content"
  req POST "/session/$SID/message" '{"content":""}' 400

  echo "3.6 Post message with numeric content (wrong type)"
  req POST "/session/$SID/message" '{"content":12345}' 400

  echo "3.7 Post message with oversized content (1MB)"
  LONG_MSG=$(python3 -c "print('X'*1048576)")
  req POST "/session/$SID/message" "{\"content\":\"$LONG_MSG\"}" 400

  echo "3.8 Get message with nonexistent messageID"
  req GET "/session/$SID/message/nonexistent-msg-id" "" 404

  echo "3.9 Get messages on valid session"
  req GET "/session/$SID/message" "" 200
fi

# ============================================================
# 4. ABORT SAD PATHS
# ============================================================
echo ""
echo "═══ 4. Abort Sad Paths ═══"

if [[ "$SID" != "SKIP" ]]; then
  echo "4.1 Abort when no generation running"
  req POST "/session/$SID/abort" "" 200

  echo "4.2 Abort twice in succession (idempotency)"
  req POST "/session/$SID/abort" "" 200
  req POST "/session/$SID/abort" "" 200

  echo "4.3 Abort with body (should not crash)"
  req POST "/session/$SID/abort" '{"extra":"data"}' 200
fi

# ============================================================
# 5. SSE / EVENT SAD PATHS
# ============================================================
echo ""
echo "═══ 5. SSE / Event Sad Paths ═══"

echo "5.1 GET /global/event (SSE stream connect+disconnect)"
CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$BASE/global/event" 2>/dev/null || echo "000")
TOTAL=$((TOTAL+1))
echo "  ℹ️  GET /global/event → $CODE (timeout/disconnect expected)"

echo "5.2 GET /event (SSE stream connect+disconnect)"
CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$BASE/event" 2>/dev/null || echo "000")
TOTAL=$((TOTAL+1))
echo "  ℹ️  GET /event → $CODE (timeout/disconnect expected)"

# ============================================================
# 6. V2 API SAD PATHS
# ============================================================
echo ""
echo "═══ 6. V2 API Sad Paths ═══"

echo "6.1 V2 session context on nonexistent session"
req GET "/api/session/ses_nonexistent000000000/context" "" 404

echo "6.2 V2 session compact on nonexistent session"
req POST "/api/session/ses_nonexistent000000000/compact" "" 404

echo "6.3 V2 permission reply on nonexistent session"
req POST "/api/session/ses_nonexistent000000000/permission/request/fake/reply" '{"allow":true}' 404

echo "6.4 V2 session messages on nonexistent session"
req GET "/api/session/ses_nonexistent000000000/message" "" 404

# ============================================================
# 7. VCS / WORKTREE SAD PATHS
# ============================================================
echo ""
echo "═══ 7. VCS / Worktree Sad Paths ═══"

echo "7.1 VCS status (should always work)"
req GET "/vcs/status" "" 200

echo "7.2 VCS diff (should always work)"
req GET "/vcs/diff" "" 200

echo "7.3 VCS diff raw"
req GET "/vcs/diff/raw" "" 200

echo "7.4 VCS apply with empty body"
req POST "/vcs/apply" "" 400

echo "7.5 VCS apply with invalid diff"
req POST "/vcs/apply" '{"diff":"not-a-diff"}' 200

# ============================================================
# 8. CONFIG / PROVIDER SAD PATHS
# ============================================================
echo ""
echo "═══ 8. Config / Provider Sad Paths ═══"

echo "8.1 GET /config (should always work)"
req GET "/config" "" 200

echo "8.2 PATCH /config with invalid JSON"
req PATCH "/config" 'NOT_JSON' 400

echo "8.3 GET /provider"
req GET "/provider" "" 200

echo "8.4 GET /config/providers"
req GET "/config/providers" "" 200

# ============================================================
# 9. MISC ENDPOINTS
# ============================================================
echo ""
echo "═══ 9. Misc Endpoints ═══"

echo "9.1 GET /path"
req GET "/path" "" 200

echo "9.2 GET /project/current"
req GET "/project/current" "" 200

echo "9.3 GET /mcp"
req GET "/mcp" "" 200

echo "9.4 GET /pty"
req GET "/pty" "" 200

echo "9.5 GET /agent"
req GET "/agent" "" 200

echo "9.6 GET /tui/control/next (short timeout)"
CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$BASE/tui/control/next" 2>/dev/null || echo "000")
TOTAL=$((TOTAL+1))
echo "  ℹ️  GET /tui/control/next → $CODE (timeout expected)"

echo "9.7 POST /log with empty body"
req POST "/log" "" 200

# ============================================================
# CLEANUP
# ============================================================
if [[ "$SID" != "SKIP" ]]; then
  echo ""
  echo "═══ Cleanup ═══"
  echo "Deleting test session $SID"
  req DELETE "/session/$SID" "" 200
fi

# ============================================================
# SUMMARY
# ============================================================
echo ""
echo "════════════════════════════════════════"
echo "  RESULTS: $PASS/$TOTAL passed, $FAIL failed"
echo "════════════════════════════════════════"
exit $FAIL
