#!/usr/bin/env bash
set -euo pipefail

WEB_BASE="${WEB_BASE:-http://localhost:5173}"
API_BASE="${API_BASE:-http://localhost:8080}"
API="${API_BASE}/api/v1"

need() { command -v "$1" >/dev/null || { echo "missing: $1"; exit 1; }; }
need curl
need jq
need dig

retry() {
  local n=0 max=12 delay=3
  until "$@"; do
    n=$((n+1))
    if [[ $n -ge $max ]]; then
      return 1
    fi
    sleep "$delay"
  done
}

echo "==> ready (api)"
READY_URL="${API_BASE}/health/ready"

if ! retry curl -fsS "$READY_URL" >/dev/null; then
  echo "ERROR: ready check failed after retries: $READY_URL"
  echo "--- dig api.onyxchat.dev"
  dig +short api.onyxchat.dev || true
  echo "--- headers"
  curl -sS -D - -o /dev/null "$READY_URL" || true
  exit 1
fi

curl -fsS "$READY_URL" | cat; echo

echo "==> register"
USER="smoke_$(date +%s)"
PASS="pass1234!"

RESP="$(curl -fsS -X POST "${API}/register" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${USER}\",\"password\":\"${PASS}\"}")"

echo "${RESP}" | cat; echo

TOKEN="$(echo "${RESP}" | jq -r .token)"
[[ -n "$TOKEN" && "$TOKEN" != "null" ]] || {
  echo "ERROR: no token returned"
  exit 1
}

echo "==> list users (auth)"
curl -fsS "${API}/users" \
  -H "Authorization: Bearer ${TOKEN}" | cat
echo

echo "✅ smoke ok"
