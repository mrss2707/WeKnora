#!/usr/bin/env python3
"""SIGKILL only the host E2E app; verify natural Dreamer lease expiry/recovery."""
import datetime
import json
import os
import pathlib
import signal
import subprocess
import time
import urllib.request
import uuid

root = pathlib.Path(__file__).resolve().parent
world = json.loads((root / 'seed-output.json').read_text())
assert world['base_url'] == 'http://localhost:18080', 'only the isolated host stack is supported'
kb = str(uuid.UUID(world['knowledge_bases']['acme-eng']['id']))
run = root / '.run'


def sql(statement):
    return subprocess.check_output([
        'docker', 'exec', 'memv2-e2e-pg', 'psql', '-X', '-v', 'ON_ERROR_STOP=1',
        '-U', 'postgres', '-d', 'weknora_e2e', '-tAc', statement,
    ], text=True).strip()


def request(method, resource, data=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    body = json.dumps(data).encode() if data is not None else None
    req = urllib.request.Request(world['base_url'] + '/api/v1' + resource, body, headers, method=method)
    with urllib.request.urlopen(req, timeout=120) as response:
        return json.loads(response.read())


def restart(short=False):
    env = os.environ.copy()
    env['MEMORY_V2_DREAMER_INTERVAL'] = '5s' if short else '6h'
    env['MEMORY_V2_WORKER_MUTATIONS'] = 'true'
    env['MEMORY_V2_DREAMER_ENABLED'] = 'true'
    env.update({
        'MEMV2_E2E_APP_PORT': '18080', 'MEMV2_E2E_PG_PORT': '55441',
        'MEMV2_E2E_REDIS_PORT': '56380', 'MEMV2_E2E_WEB_PORT': '15173',
    })
    with (run / 'dreamer-restart.log').open('a') as log:
        subprocess.run([str(root / 'stack.sh'), 'restart-app'], env=env, stdout=log, stderr=subprocess.STDOUT, check=True)


def state():
    raw = sql(f"SELECT row_to_json(s) FROM (SELECT locked_by, locked_until, runs, heartbeat_at FROM dreamer_state WHERE kb_id = '{kb}') s")
    return json.loads(raw) if raw else {}


def invariants():
    return sql(f"SELECT json_build_object('live', count(*), 'protected', jsonb_agg(jsonb_build_array(id, content, verdict, importance, tier) ORDER BY id) FILTER (WHERE verdict IN ('decision','fixed'))) FROM agent_memories WHERE kb_id = '{kb}' AND deleted_at IS NULL")


login = request('POST', '/auth/login', {'email': world['users']['alice']['email'], 'password': world['password']})
token = login['token']
sentinel = str(uuid.UUID(world['memories']['acme-eng']['vi_minio']))
original = request('GET', '/memories/' + sentinel, token=token)['data']
before = invariants()
try:
    restart(short=True)
    current = request('GET', '/memories/' + sentinel, token=token)['data']
    request('PUT', '/memories/' + sentinel, {
        'tags': current['tags'] + ['restart-smoke-' + str(uuid.uuid4())],
        'expected_updated_at': current['updated_at'],
    }, token)
    deadline = time.monotonic() + 60
    while True:
        held = state()
        if held.get('locked_by') and held.get('locked_until'):
            pid = int((run / 'app.pid').read_text().strip())
            command = subprocess.check_output(['ps', '-p', str(pid), '-o', 'command='], text=True).strip()
            assert command == str(run / 'weknora-server'), 'refusing to kill an unrecognized process'
            os.kill(pid, signal.SIGKILL)
            break
        assert time.monotonic() < deadline, 'no active Dreamer pass observed'
        time.sleep(0.1)
    crashed = state()
    assert crashed.get('locked_by') == held['locked_by'], 'pass finished before SIGKILL; crash evidence is inconclusive'
    expiry = datetime.datetime.fromisoformat(crashed['locked_until']).timestamp()
    print('PASS: SIGKILL left an active, persisted Dreamer lease', flush=True)
    restart(short=True)
    deadline = time.monotonic() + max(0, expiry - time.time()) + 120
    while True:
        recovered = state()
        if recovered.get('runs', 0) > crashed['runs'] and not recovered.get('locked_by'):
            assert time.time() >= expiry, 'pass completed before the crashed lease expired'
            break
        if time.time() < expiry:
            assert recovered.get('locked_by') == crashed['locked_by'], 'unexpired lease was stolen'
            assert recovered.get('heartbeat_at') == crashed['heartbeat_at'], 'dead worker heartbeat unexpectedly advanced'
        assert time.monotonic() < deadline, 'Dreamer did not recover after natural lease expiry'
        time.sleep(2)
    assert invariants() == before, 'live row count or protected memories changed'
    assert sql("SELECT count(*) FROM memory_relations r JOIN agent_memories a ON a.id=r.from_uuid JOIN agent_memories b ON b.id=r.to_uuid WHERE r.deleted_at IS NULL AND (a.tenant_id <> b.tenant_id OR a.kb_id <> b.kb_id OR a.deleted_at IS NOT NULL OR b.deleted_at IS NOT NULL)") == '0'
    print('PASS: natural lease expiry, scheduled recovery, unchanged live count/protected rows and safe graph', flush=True)
finally:
    restart()
    current = request('GET', '/memories/' + sentinel, token=token)['data']
    request('PUT', '/memories/' + sentinel, {
        'tags': original['tags'], 'expected_updated_at': current['updated_at'],
    }, token)
    print('PASS: original sentinel tags and default 6h worker interval restored', flush=True)
