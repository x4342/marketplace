#!/usr/bin/env bash
set -euo pipefail
mkdir -p bin/smoke

./bin/web >bin/smoke/web.log 2>&1 &
web_pid=$!
worker_pid=
cleanup() {
  if [[ -n "$worker_pid" ]]; then
    kill "$worker_pid" 2>/dev/null || true
    wait "$worker_pid" 2>/dev/null || true
  fi
  kill "$web_pid" 2>/dev/null || true
  wait "$web_pid" 2>/dev/null || true
}
trap cleanup EXIT

export MARKETPLACE_SMOKE_URL="http://${HTTP_ADDR:-127.0.0.1:8080}"
wait_for_page() {
  local path=$1
  local expected=$2
  for _ in {1..30}; do
    if curl -fs --max-time 2 "$MARKETPLACE_SMOKE_URL$path" | grep -Fq "$expected"; then
      return 0
    fi
    sleep 1
  done
  cat bin/smoke/web.log
  return 1
}

wait_for_page /health/live ok
wait_for_page /health/ready ready
wait_for_page /discover 'Discover'
worker_active() {
  psql "$DATABASE_URL" -XAtc "SELECT EXISTS (SELECT 1 FROM worker_heartbeat WHERE observed_at >= now() - interval '45 seconds')" | grep -qx t
}
./bin/worker >bin/smoke/worker.log 2>&1 &
worker_pid=$!
for _ in {1..30}; do
  if worker_active; then
    break
  fi
  sleep 1
done
worker_active
python scripts/browser_smoke.py
