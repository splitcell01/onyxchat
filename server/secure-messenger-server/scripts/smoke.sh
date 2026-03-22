#!/usr/bin/env bash
# smoke.sh — end-to-end smoke test against a running OnyxChat server.
#
# Usage (local):
#   SMOKE_INVITE_CODE=smoke-ci SMOKE_INVITE_CODE_2=smoke-ci-2 bash scripts/smoke.sh
#
# Usage (prod):
#   API_BASE=https://api.onyxchat.dev \
#   SMOKE_INVITE_CODE=smoke-ci SMOKE_INVITE_CODE_2=smoke-ci-2 bash scripts/smoke.sh
#
# Seed both codes once:
#   INSERT INTO invite_codes (code, created_by)
#   VALUES ('smoke-ci', 'ci'), ('smoke-ci-2', 'ci');
#
# Re-seed before each CI run:
#   INSERT INTO invite_codes (code, created_by)
#   VALUES ('smoke-ci', 'ci'), ('smoke-ci-2', 'ci')
#   ON CONFLICT (code) DO UPDATE SET used_by = NULL, used_at = NULL;

set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080}"
API="${API_BASE}/api/v1"

# ── Preflight ─────────────────────────────────────────────────
need() { command -v "$1" >/dev/null || { echo "ERROR: missing tool: $1"; exit 1; }; }
need curl
need jq

[[ -n "${SMOKE_INVITE_CODE:-}" ]] || { echo "ERROR: SMOKE_INVITE_CODE is not set."; exit 1; }
[[ -n "${SMOKE_INVITE_CODE_2:-}" ]] || { echo "ERROR: SMOKE_INVITE_CODE_2 is not set."; exit 1; }

# ── Helpers ───────────────────────────────────────────────────
retry() {
  local n=0 max=12 delay=3
  until "$@"; do
    n=$((n+1)); [[ $n -ge $max ]] && return 1; sleep "$delay"
  done
}

check() {
  local desc="$1" got="$2" want="$3"
  if [[ "$got" != "$want" ]]; then
    echo "FAIL [$desc]: expected '$want', got '$got'"; exit 1
  fi
  echo "  ✓ $desc"
}

# ── 1. Readiness ──────────────────────────────────────────────
echo "==> health/ready"
READY_URL="${API_BASE}/health/ready"
if ! retry curl -fsS "$READY_URL" >/dev/null; then
  echo "ERROR: server never became ready at $READY_URL"
  curl -sS -D - -o /dev/null "$READY_URL" || true
  exit 1
fi
STATUS="$(curl -fsS "$READY_URL" | jq -r '.status')"
check "ready status" "$STATUS" "ready"

# ── 2. Register primary user + peer ──────────────────────────
echo "==> register"
PASS="Smoke!Pass1"
USER="smoke_$(date +%s%N | md5sum | head -c8)"
PEER="smoke_$(date +%s%N | md5sum | head -c8)"

REG_RESP="$(curl -fsS -X POST "${API}/register" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${USER}\",\"password\":\"${PASS}\",\"invite_code\":\"${SMOKE_INVITE_CODE}\"}")"

TOKEN="$(echo "${REG_RESP}" | jq -r '.token')"
USER_ID="$(echo "${REG_RESP}" | jq -r '.id')"
[[ -n "$TOKEN" && "$TOKEN" != "null" ]] || { echo "ERROR: register returned no token"; echo "${REG_RESP}" | jq .; exit 1; }
check "register returns token"    "ok" "ok"
check "register returns id"       "$(echo "${REG_RESP}" | jq -r '.id | type')" "number"
check "register returns username" "$(echo "${REG_RESP}" | jq -r '.username')" "$USER"

PEER_RESP="$(curl -fsS -X POST "${API}/register" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${PEER}\",\"password\":\"${PASS}\",\"invite_code\":\"${SMOKE_INVITE_CODE_2}\"}")"
[[ "$(echo "${PEER_RESP}" | jq -r '.token')" != "null" ]] || { echo "ERROR: peer registration failed"; echo "${PEER_RESP}" | jq .; exit 1; }
check "peer registered" "ok" "ok"

# ── 3. Login ──────────────────────────────────────────────────
echo "==> login"
LOGIN_RESP="$(curl -fsS -X POST "${API}/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${USER}\",\"password\":\"${PASS}\"}")"
LOGIN_TOKEN="$(echo "${LOGIN_RESP}" | jq -r '.token')"
[[ -n "$LOGIN_TOKEN" && "$LOGIN_TOKEN" != "null" ]] || { echo "ERROR: login returned no token"; exit 1; }
check "login returns token" "ok" "ok"
TOKEN="$LOGIN_TOKEN"

# ── 4. List users (authenticated) ────────────────────────────
echo "==> list users"
USERS_RESP="$(curl -fsS "${API}/users" -H "Authorization: Bearer ${TOKEN}")"
USERS_COUNT="$(echo "${USERS_RESP}" | jq 'length')"
[[ "$USERS_COUNT" -ge 1 ]] || { echo "ERROR: expected at least 1 user, got ${USERS_COUNT}"; exit 1; }
check "users list non-empty" "ok" "ok"

# ── 5. List users — unauthorized ─────────────────────────────
echo "==> list users (no auth → 401)"
HTTP_CODE="$(curl -o /dev/null -w '%{http_code}' -s "${API}/users")"
check "unauthenticated → 401" "$HTTP_CODE" "401"

# ── 6. Send a message to peer ────────────────────────────────
echo "==> send message"
CLIENT_ID="smoke-$(date +%s%N)"
SEND_RESP_CODE="$(curl -o /dev/null -w '%{http_code}' -s \
  -X POST "${API}/messages" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"recipientUsername\":\"${PEER}\",\"body\":\"hello smoke\",\"clientMessageId\":\"${CLIENT_ID}\"}")"
check "send message → 201" "$SEND_RESP_CODE" "201"

# ── 7. Idempotent resend (same clientMessageId → 200) ────────
echo "==> send message (duplicate → 200)"
RESEND_CODE="$(curl -o /dev/null -w '%{http_code}' -s \
  -X POST "${API}/messages" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"recipientUsername\":\"${PEER}\",\"body\":\"hello smoke\",\"clientMessageId\":\"${CLIENT_ID}\"}")"
check "duplicate send → 200" "$RESEND_CODE" "200"

# ── 8. List messages ──────────────────────────────────────────
echo "==> list messages"
MSGS_RESP="$(curl -fsS "${API}/messages?peer=${PEER}" -H "Authorization: Bearer ${TOKEN}")"
MSG_COUNT="$(echo "${MSGS_RESP}" | jq '.messages | length')"
[[ "$MSG_COUNT" -ge 1 ]] || { echo "ERROR: expected at least 1 message, got ${MSG_COUNT}"; exit 1; }
check "messages list non-empty" "ok" "ok"

# ── Done ──────────────────────────────────────────────────────
echo ""
echo "✅  smoke ok  (user=${USER} id=${USER_ID})"
