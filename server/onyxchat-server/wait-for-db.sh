#!/usr/bin/env sh
set -eu

# If no DB is configured, just run the server.
if [ -z "${SM_DB_DSN:-}" ]; then
  echo "SM_DB_DSN not set; skipping DB wait"
  exec "$@"
fi

# Parse host/port from SM_DB_DSN without extra deps.
# Works for: postgres://user:pass@host:5432/db?sslmode=disable
DSN="$SM_DB_DSN"

HOST="$(echo "$DSN" | sed -n 's#.*@\(.*\):\([0-9][0-9]*\)/.*#\1#p')"
PORT="$(echo "$DSN" | sed -n 's#.*@\(.*\):\([0-9][0-9]*\)/.*#\2#p')"

# Fallback defaults if parsing fails (common for localhost dev)
HOST="${HOST:-db}"
PORT="${PORT:-5432}"

echo "Waiting for Postgres at ${HOST}:${PORT} ..."

i=0
while ! nc -z -w 1 "$HOST" "$PORT" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then
    echo "ERROR: DB not reachable after 60s at ${HOST}:${PORT}"
    exit 1
  fi
  sleep 1
done

echo "DB is reachable ✅"
exec "$@"
