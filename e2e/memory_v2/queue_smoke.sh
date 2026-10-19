#!/usr/bin/env bash
# Verify durable graph intent survives a host app restart. Only this isolated
# stack is paused; no production/developer container is stopped or reconfigured.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RUN="$ROOT/e2e/memory_v2/.run"
STATE="$RUN/queue-smoke-$(date +%s)-$$.json"
RESTORE=true
mkdir -p "$RUN"
cleanup() {
  local code=$?
  trap - EXIT
  if [ "$RESTORE" = true ]; then
    if ! MEMORY_V2_WORKER_MUTATIONS=true MEMORY_V2_DREAMER_ENABLED=true "$ROOT/e2e/memory_v2/stack.sh" restart-app >"$RUN/queue-smoke-restore.log" 2>&1; then
      printf 'failed to restore test workers; see %s/queue-smoke-restore.log\n' "$RUN" >&2
      code=1
    fi
  fi
  if [ -f "$STATE" ]; then
    if ! python3 -I "$ROOT/e2e/memory_v2/queue_smoke.py" cleanup "$STATE" >>"$RUN/queue-smoke-restore.log" 2>&1; then
      printf 'test memory cleanup failed; retained state at %s\n' "$STATE" >&2
      code=1
    fi
  fi
  exit "$code"
}
# Refuse a non-host seed before stopping or rebuilding any test process.
python3 -I -c 'import json,sys; w=json.load(open(sys.argv[1])); assert w["base_url"] == "http://localhost:18080", "only the isolated host stack is supported"' "$ROOT/e2e/memory_v2/seed-output.json"
export MEMV2_E2E_APP_PORT=18080 MEMV2_E2E_PG_PORT=55441 MEMV2_E2E_REDIS_PORT=56380 MEMV2_E2E_WEB_PORT=15173
trap cleanup EXIT
MEMORY_V2_WORKER_MUTATIONS=false "$ROOT/e2e/memory_v2/stack.sh" restart-app >"$RUN/queue-smoke-paused.log" 2>&1
python3 -I "$ROOT/e2e/memory_v2/queue_smoke.py" before "$STATE"
ID="$(python3 -I -c 'import json,sys,uuid;print(uuid.UUID(json.load(open(sys.argv[1]))["id"]))' "$STATE")"
status="$(docker exec memv2-e2e-pg psql -U postgres -d weknora_e2e -tAc "SELECT status FROM extraction_queue WHERE memory_uuid='$ID' AND job_type='graph_link'")"
[ "$status" = pending ] || { printf 'expected pending durable job, got %s\n' "$status" >&2; exit 1; }
MEMORY_V2_WORKER_MUTATIONS=true MEMORY_V2_DREAMER_ENABLED=true "$ROOT/e2e/memory_v2/stack.sh" restart-app >"$RUN/queue-smoke-resumed.log" 2>&1
RESTORE=false
deadline=$((SECONDS+30))
while true; do
  status="$(docker exec memv2-e2e-pg psql -U postgres -d weknora_e2e -tAc "SELECT status FROM extraction_queue WHERE memory_uuid='$ID' AND job_type='graph_link'")"
  [ "$status" != failed ] || { printf 'graph job failed after restart\n' >&2; exit 1; }
  [ "$status" != done ] || break
  [ "$SECONDS" -lt "$deadline" ] || { printf 'graph job did not complete after restart: %s\n' "$status" >&2; exit 1; }
  sleep 1
done
python3 -I "$ROOT/e2e/memory_v2/queue_smoke.py" after "$STATE"
printf 'PASS: pending graph job survived process restart and completed with workers enabled\n'
