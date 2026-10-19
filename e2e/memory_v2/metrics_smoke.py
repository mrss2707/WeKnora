#!/usr/bin/env python3
"""Verify real metrics authorization; only the isolated host E2E database is used."""
import json
import pathlib
import re
import subprocess
import urllib.error
import urllib.request

root = pathlib.Path(__file__).parent
world = json.loads((root / 'seed-output.json').read_text())
assert world['base_url'] == 'http://localhost:18080', 'only the isolated host stack is supported'
email = world['users']['alice']['email']
assert email == 'alice@memv2-e2e.test', 'refusing a non-fixture account'


def sql(statement):
    return subprocess.check_output([
        'docker', 'exec', 'memv2-e2e-pg', 'psql', '-X', '-v', 'ON_ERROR_STOP=1',
        '-U', 'postgres', '-d', 'weknora_e2e', '-tAc', statement,
    ], text=True).strip()


def request(resource, token=None, data=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    body = json.dumps(data).encode() if data is not None else None
    req = urllib.request.Request(world['base_url'] + '/api/v1' + resource, body, headers)
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            return response.status, response.read().decode()
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode()


status, raw = request('/auth/login', data={'email': email, 'password': world['password']})
assert status == 200, 'fixture login failed'
token = json.loads(raw)['token']
assert request('/memories/metrics')[0] == 401, 'anonymous metrics must be rejected'
original = sql("SELECT is_system_admin FROM users WHERE email = 'alice@memv2-e2e.test'")
assert original == 'f', 'fixture owner must initially not be a SystemAdmin'
assert request('/memories/metrics', token)[0] == 403, 'tenant Owner must not read global metrics'
try:
    sql("UPDATE users SET is_system_admin = true WHERE email = 'alice@memv2-e2e.test'")
    status, metrics = request('/memories/metrics', token)
    assert status == 200, 'SystemAdmin metrics request failed'
    samples = [line for line in metrics.splitlines() if line and not line.startswith('#')]
    assert samples and all(line.startswith('weknora_memory_') for line in samples)
    for labels in re.findall(r'\{([^}]*)\}', metrics):
        names = set(re.findall(r'(\w+)\s*=', labels))
        assert names <= {'worker', 'outcome', 'operation', 'phase', 'le'}, 'unbounded or sensitive metric label'
    assert token not in metrics and email not in metrics, 'credential or identity leaked in metrics'
    print('PASS: anonymous 401, tenant Owner 403, SystemAdmin 200; bounded content-free metrics')
finally:
    sql("UPDATE users SET is_system_admin = false WHERE email = 'alice@memv2-e2e.test'")
    assert sql("SELECT is_system_admin FROM users WHERE email = 'alice@memv2-e2e.test'") == 'f'
    assert request('/memories/metrics', token)[0] == 403, 'privilege revocation must apply immediately'
    print('PASS: original fixture privileges restored; metrics denied again')
