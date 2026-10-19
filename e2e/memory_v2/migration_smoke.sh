#!/usr/bin/env bash
# Fresh startup and upgrade of an isolated clone, never a developer database.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RUN="$ROOT/e2e/memory_v2/.run"
SUFFIX="$(date +%s)_$$"
FRESH="memv2_migration_fresh_$SUFFIX"
UPGRADE="memv2_migration_upgrade_$SUFFIX"
PORT=18081
PID=""
cleanup() {
  if [ -n "$PID" ]; then kill "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true; fi
  docker exec memv2-e2e-pg dropdb --if-exists --force -U postgres "$FRESH" >/dev/null 2>&1 || true
  docker exec memv2-e2e-pg dropdb --if-exists --force -U postgres "$UPGRADE" >/dev/null 2>&1 || true
}
trap cleanup EXIT
mkdir -p "$RUN"
# Do not mistake another process's health response for this smoke server.
python3 - "$PORT" <<'PY'
import socket,sys
s=socket.socket(); s.bind(('127.0.0.1',int(sys.argv[1]))); s.close()
PY
(cd "$ROOT" && go build -o "$RUN/migration-server" ./cmd/server)
docker exec memv2-e2e-pg createdb -U postgres "$FRESH"
docker exec memv2-e2e-pg createdb -U postgres -T template0 "$UPGRADE"
docker exec memv2-e2e-pg pg_dump -U postgres -Fc weknora_e2e | docker exec -i memv2-e2e-pg pg_restore -U postgres --no-owner -d "$UPGRADE"
# Reconstruct the previous module schema only inside the disposable clone.
docker exec -i memv2-e2e-pg psql -v ON_ERROR_STOP=1 -U postgres -d "$UPGRADE" < "$ROOT/migrations/modules/memory_v2/postgres/900084_memory_v2_scope_settings.down.sql" >/dev/null
docker exec -i memv2-e2e-pg psql -v ON_ERROR_STOP=1 -U postgres -d "$UPGRADE" < "$ROOT/migrations/modules/memory_v2/postgres/900083_memory_v2_graph_jobs.down.sql" >/dev/null
docker exec memv2-e2e-pg psql -v ON_ERROR_STOP=1 -U postgres -d "$UPGRADE" -c 'DROP TABLE memory_v2_dreamer_budget; UPDATE schema_migrations_modules SET version=900081, dirty=false;' >/dev/null
docker exec memv2-e2e-pg psql -v ON_ERROR_STOP=1 -U postgres -d "$UPGRADE" -c "UPDATE agent_memories SET metadata=COALESCE(metadata,'{}'::jsonb)-'embedding_model_id' WHERE id IN (SELECT m.id FROM agent_memories m JOIN knowledge_bases kb ON kb.id=m.kb_id WHERE kb.name='acme-eng' AND m.deleted_at IS NULL ORDER BY (m.verdict IN ('decision','fixed')) DESC,m.id LIMIT 3);" >/dev/null
for DB in "$FRESH" "$UPGRADE"; do
  (
    cd "$ROOT"
    export GIN_MODE=release LOG_LEVEL=info AUTO_MIGRATE=true SERVER_PORT="$PORT"
    export DB_DRIVER=postgres DB_HOST=localhost DB_PORT=55441 DB_USER=postgres DB_PASSWORD=e2e DB_NAME="$DB"
    export RETRIEVE_DRIVER=postgres STORAGE_TYPE=local LOCAL_STORAGE_BASE_DIR="$RUN/migration-files"
    export REDIS_ADDR=localhost:56380 REDIS_DB=14 STREAM_MANAGER_TYPE=redis
    export DOCREADER_ADDR=localhost:1 OLLAMA_OPTIONAL=true OLLAMA_BASE_URL=http://localhost:11434
    export JWT_SECRET=memv2-e2e-jwt-secret-0123456789abcdef SYSTEM_AES_KEY=0123456789abcdef0123456789abcdef
    export SSRF_WHITELIST=localhost,127.0.0.1 MEMORY_V2_WORKER_MUTATIONS=false MEMORY_V2_DREAMER_ENABLED=false
    exec "$RUN/migration-server"
  ) >"$RUN/$DB.log" 2>&1 &
  PID=$!
  READY=false
  for _ in $(seq 1 90); do
    if curl -fsS "http://localhost:$PORT/health" >/dev/null 2>&1; then READY=true; break; fi
    kill -0 "$PID" 2>/dev/null || break
    sleep 1
  done
  [ "$READY" = true ] || { printf 'startup failed: %s\n' "$RUN/$DB.log" >&2; exit 1; }
  VERSION="$(docker exec memv2-e2e-pg psql -U postgres -d "$DB" -tAc "SELECT version || ':' || dirty FROM schema_migrations_modules")"
  [ "$VERSION" = '900084:false' ] || { printf 'unexpected module version: %s\n' "$VERSION" >&2; exit 1; }
  docker exec memv2-e2e-pg psql -U postgres -d "$DB" -c 'SELECT * FROM schema_migrations; SELECT * FROM schema_migrations_modules; SELECT COUNT(*) AS budget_rows FROM memory_v2_dreamer_budget; SELECT COUNT(*) AS settings_rows FROM memory_v2_scope_settings;'
  if [ "$DB" = "$UPGRADE" ]; then
    python3 "$ROOT/e2e/memory_v2/backfill_smoke.py" "http://localhost:$PORT"
  fi
  kill "$PID"; wait "$PID" 2>/dev/null || true; PID=""
  printf 'PASS: %s\n' "$DB"
done
