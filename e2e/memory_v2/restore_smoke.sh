#!/usr/bin/env bash
# Backup/restore rehearsal of the isolated E2E database only (never a developer
# or production database). Dumps weknora_e2e, restores it into a disposable
# database, starts the server on the copy with mutating workers off, and checks
# that Memory V2 state, vectors, audit, budget, settings and the migration
# version survived; finally compares a scoped REST search on both servers.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RUN="$ROOT/e2e/memory_v2/.run"
SOURCE=weknora_e2e
TARGET="memv2_restore_$(date +%s)_$$"
PORT=18083
PID=""
cleanup() {
  if [ -n "$PID" ]; then kill "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true; fi
  docker exec memv2-e2e-pg dropdb --if-exists --force -U postgres "$TARGET" >/dev/null 2>&1 || true
  rm -f "$RUN/restore.dump"
}
trap cleanup EXIT
case "$TARGET" in memv2_restore_*) ;; *) echo "refusing non-disposable target" >&2; exit 1 ;; esac
python3 - "$PORT" <<'PY'
import socket,sys
s=socket.socket(); s.bind(('127.0.0.1',int(sys.argv[1]))); s.close()
PY

STATE_SQL="SELECT json_build_object(
  'memories', (SELECT count(*) FROM agent_memories WHERE deleted_at IS NULL),
  'memories_all', (SELECT count(*) FROM agent_memories),
  'with_embedding', (SELECT count(*) FROM agent_memories WHERE embedding IS NOT NULL),
  'embedding_models', (SELECT count(DISTINCT metadata->>'embedding_model_id') FROM agent_memories WHERE deleted_at IS NULL),
  'content_md5', (SELECT md5(string_agg(id || ':' || md5(content) || ':' || verdict || ':' || importance, ',' ORDER BY id)) FROM agent_memories),
  'embedding_md5', (SELECT md5(string_agg(id || ':' || md5(embedding::text), ',' ORDER BY id)) FROM agent_memories WHERE embedding IS NOT NULL),
  'audit', (SELECT count(*) FROM agent_memories_audit),
  'relations', (SELECT count(*) FROM memory_relations),
  'queue', (SELECT count(*) FROM extraction_queue),
  'dreamer_state', (SELECT count(*) FROM dreamer_state),
  'budget_tokens', (SELECT COALESCE(sum(tokens),0) FROM memory_v2_dreamer_budget),
  'settings', (SELECT count(*) FROM memory_v2_scope_settings),
  'core', (SELECT version || ':' || dirty FROM schema_migrations),
  'module', (SELECT version || ':' || dirty FROM schema_migrations_modules))"

state() { docker exec memv2-e2e-pg psql -U postgres -d "$1" -tAc "$STATE_SQL"; }

started=$(date +%s)
docker exec memv2-e2e-pg pg_dump -U postgres -Fc "$SOURCE" > "$RUN/restore.dump"
BEFORE="$(state "$SOURCE")"
dumped=$(date +%s)
docker exec memv2-e2e-pg createdb -U postgres -T template0 "$TARGET"
docker exec -i memv2-e2e-pg pg_restore -U postgres --no-owner --exit-on-error -d "$TARGET" < "$RUN/restore.dump"
restored=$(date +%s)
AFTER="$(state "$TARGET")"
printf 'dump=%ss restore=%ss size=%s\n' "$((dumped-started))" "$((restored-dumped))" "$(du -h "$RUN/restore.dump" | cut -f1)"
python3 - "$BEFORE" "$AFTER" <<'PY'
import json,sys
before,after=json.loads(sys.argv[1]),json.loads(sys.argv[2])
diff={k:(before[k],after.get(k)) for k in before if before[k]!=after.get(k)}
print(json.dumps(before))
if diff:
    print("state mismatch:", diff); sys.exit(1)
print("state identical after restore")
PY

(cd "$ROOT" && go build -o "$RUN/restore-server" ./cmd/server)
(
  cd "$ROOT"
  export GIN_MODE=release LOG_LEVEL=info AUTO_MIGRATE=true SERVER_PORT="$PORT"
  export DB_DRIVER=postgres DB_HOST=localhost DB_PORT=55441 DB_USER=postgres DB_PASSWORD=e2e DB_NAME="$TARGET"
  export RETRIEVE_DRIVER=postgres STORAGE_TYPE=local LOCAL_STORAGE_BASE_DIR="$RUN/files"
  export REDIS_ADDR=localhost:56380 REDIS_DB=13 STREAM_MANAGER_TYPE=redis
  export DOCREADER_ADDR=localhost:1 OLLAMA_OPTIONAL=true OLLAMA_BASE_URL=http://localhost:11434
  export JWT_SECRET=memv2-e2e-jwt-secret-0123456789abcdef SYSTEM_AES_KEY=0123456789abcdef0123456789abcdef
  export SSRF_WHITELIST=localhost,127.0.0.1 MEMORY_V2_WORKER_MUTATIONS=false MEMORY_V2_DREAMER_ENABLED=false
  exec "$RUN/restore-server"
) >"$RUN/restore-server.log" 2>&1 &
PID=$!
READY=false
for _ in $(seq 1 90); do
  if curl -fsS "http://localhost:$PORT/health" >/dev/null 2>&1; then READY=true; break; fi
  kill -0 "$PID" 2>/dev/null || break
  sleep 1
done
[ "$READY" = true ] || { echo "restored server failed to start: $RUN/restore-server.log" >&2; exit 1; }
[ "$(state "$TARGET")" = "$AFTER" ] || { echo "startup changed restored state" >&2; exit 1; }
python3 -I "$ROOT/e2e/memory_v2/restore_compare.py" "http://localhost:18080" "http://localhost:$PORT" "$ROOT/e2e/memory_v2/seed-output.json"
echo "PASS: restore rehearsal ($TARGET dropped on exit)"
