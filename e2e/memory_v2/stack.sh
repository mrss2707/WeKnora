#!/usr/bin/env bash
# Memory V2 end-to-end stack: the branch's server binary + Vite dev server
# against an isolated ParadeDB database and Redis, with real local models
# (Ollama). Nothing touches a developer database.
#
#   e2e/memory_v2/stack.sh up      # start (idempotent)
#   e2e/memory_v2/stack.sh down    # stop and drop the e2e database
#   e2e/memory_v2/stack.sh status
#
# Ports: app :18080, frontend :15173, ParadeDB :55441, Redis :56380.
# Models (override via env): MEMV2_E2E_OLLAMA (http://localhost:11434),
# MEMV2_E2E_EMBED_MODEL (bge-m3), MEMV2_E2E_CHAT_MODEL (qwen2.5:3b).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RUN="$ROOT/e2e/memory_v2/.run"
mkdir -p "$RUN"

APP_PORT="${MEMV2_E2E_APP_PORT:-18080}"
WEB_PORT="${MEMV2_E2E_WEB_PORT:-15173}"
PG_PORT="${MEMV2_E2E_PG_PORT:-55441}"
REDIS_PORT="${MEMV2_E2E_REDIS_PORT:-56380}"
PG_IMAGE="paradedb/paradedb:v0.22.6-pg17"

start_pg() {
  if ! docker ps --format '{{.Names}}' | grep -qx memv2-e2e-pg; then
    docker rm -f memv2-e2e-pg >/dev/null 2>&1 || true
    docker run -d --name memv2-e2e-pg -p "127.0.0.1:$PG_PORT:5432" \
      -e POSTGRES_PASSWORD=e2e -e POSTGRES_DB=weknora_e2e "$PG_IMAGE" >/dev/null
  fi
  # The image restarts postgres once after init; wait for a stable server
  # reachable over TCP (what the app uses), twice in a row.
  local ok=0
  for _ in $(seq 1 90); do
    if PGPASSWORD=e2e docker exec -e PGPASSWORD=e2e memv2-e2e-pg psql -h 127.0.0.1 -U postgres -d weknora_e2e -tAc 'select 1' >/dev/null 2>&1; then
      ok=$((ok + 1))
      [ "$ok" -ge 2 ] && return 0
    else
      ok=0
    fi
    sleep 1
  done
  echo "postgres did not become ready" >&2
  exit 1
}

start_redis() {
  if ! docker ps --format '{{.Names}}' | grep -qx memv2-e2e-redis; then
    docker rm -f memv2-e2e-redis >/dev/null 2>&1 || true
    docker run -d --name memv2-e2e-redis -p "127.0.0.1:$REDIS_PORT:6379" redis:7-alpine >/dev/null
  fi
}

start_app() {
  if [ -f "$RUN/app.pid" ] && kill -0 "$(cat "$RUN/app.pid")" 2>/dev/null; then return 0; fi
  (cd "$ROOT" && go build -o "$RUN/weknora-server" ./cmd/server)
  mkdir -p "$RUN/files"
  (
    cd "$ROOT"
    export GIN_MODE=release LOG_LEVEL=info AUTO_MIGRATE=true SERVER_PORT="$APP_PORT"
    export DB_DRIVER=postgres DB_HOST=localhost DB_PORT="$PG_PORT" DB_USER=postgres DB_PASSWORD=e2e DB_NAME=weknora_e2e
    export RETRIEVE_DRIVER=postgres STORAGE_TYPE=local LOCAL_STORAGE_BASE_DIR="$RUN/files"
    export REDIS_ADDR="localhost:$REDIS_PORT" STREAM_MANAGER_TYPE=redis
    export DOCREADER_ADDR=localhost:1 OLLAMA_BASE_URL="${MEMV2_E2E_OLLAMA:-http://localhost:11434}" OLLAMA_OPTIONAL=true
    export JWT_SECRET=memv2-e2e-jwt-secret-0123456789abcdef SYSTEM_AES_KEY=0123456789abcdef0123456789abcdef
    export SSRF_WHITELIST=localhost,127.0.0.1
    export MEMORY_V2_ENABLED=true MEMORY_V2_WORKER_MUTATIONS="${MEMORY_V2_WORKER_MUTATIONS:-true}" MEMORY_V2_DREAMER_ENABLED="${MEMORY_V2_DREAMER_ENABLED:-true}"
    export MEMORY_V2_WRITE_LIMIT_PER_MIN="${MEMORY_V2_WRITE_LIMIT_PER_MIN:-600}" MEMORY_V2_DREAM_COOLDOWN="${MEMORY_V2_DREAM_COOLDOWN:-5s}"
    nohup "$RUN/weknora-server" >"$RUN/app.log" 2>&1 &
    echo $! >"$RUN/app.pid"
  )
  for _ in $(seq 1 120); do
    curl -fsS "http://localhost:$APP_PORT/health" >/dev/null 2>&1 && return 0
    kill -0 "$(cat "$RUN/app.pid")" 2>/dev/null || { tail -40 "$RUN/app.log" >&2; exit 1; }
    sleep 1
  done
  echo "app did not become healthy; see $RUN/app.log" >&2
  exit 1
}

start_web() {
  if [ -f "$RUN/web.pid" ] && kill -0 "$(cat "$RUN/web.pid")" 2>/dev/null; then return 0; fi
  (
    cd "$ROOT/frontend"
    VITE_DEV_PROXY_TARGET="http://localhost:$APP_PORT" nohup node node_modules/vite/bin/vite.js --host 127.0.0.1 --port "$WEB_PORT" --strictPort >"$RUN/web.log" 2>&1 &
    echo $! >"$RUN/web.pid"
  )
  for _ in $(seq 1 90); do
    curl -fsS "http://localhost:$WEB_PORT/" >/dev/null 2>&1 && return 0
    sleep 1
  done
  echo "frontend did not start; see $RUN/web.log" >&2
  exit 1
}

kill_tree() {
  local pid="$1" child
  while read -r child; do
    [ -z "$child" ] || kill_tree "$child"
  done < <(pgrep -P "$pid" || true)
  kill "$pid" 2>/dev/null || true
}

stop_pid() {
  local name="$1" f="$RUN/$1.pid" pid command
  if [ -f "$f" ]; then
    pid="$(cat "$f")"
    command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    # A stale PID file must never terminate an unrelated process.
    if [[ "$name" == app && "$command" == *"$RUN/weknora-server"* ]] ||
       [[ "$name" == web && "$command" == *vite* && "$command" == *"$WEB_PORT"* ]]; then
      kill_tree "$pid"
      for _ in $(seq 1 100); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.1
      done
      if kill -0 "$pid" 2>/dev/null; then
        printf 'test process %s did not stop; leaving its PID file intact\n' "$pid" >&2
        return 1
      fi
    fi
    rm -f "$f"
  fi
}

case "${1:-up}" in
  up)
    start_pg
    start_redis
    start_app
    start_web
    echo "app      http://localhost:$APP_PORT"
    echo "frontend http://localhost:$WEB_PORT"
    ;;
  down)
    stop_pid web
    stop_pid app
    docker rm -f memv2-e2e-pg memv2-e2e-redis >/dev/null 2>&1 || true
    ;;
  restart-app)
    stop_pid app
    start_app
    ;;
  status)
    for p in app web; do
      if [ -f "$RUN/$p.pid" ] && kill -0 "$(cat "$RUN/$p.pid")" 2>/dev/null; then echo "$p: running"; else echo "$p: stopped"; fi
    done
    docker ps --filter name=memv2-e2e --format '{{.Names}}: {{.Status}}'
    ;;
  *)
    echo "usage: $0 up|down|restart-app|status" >&2
    exit 2
    ;;
esac
