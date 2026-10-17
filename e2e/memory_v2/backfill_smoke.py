#!/usr/bin/env python3
"""Public-API backfill verification against migration_smoke.sh's disposable clone."""
import json
import pathlib
import sys
import urllib.request

base = sys.argv[1]
world = json.loads((pathlib.Path(__file__).parent / 'seed-output.json').read_text())

def request(method, path, data=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    body = json.dumps(data).encode() if data is not None else None
    with urllib.request.urlopen(urllib.request.Request(base + '/api/v1' + path, body, headers, method=method), timeout=120) as response:
        return json.load(response)

login = request('POST', '/auth/login', {'email': world['users']['alice']['email'], 'password': world['password']})
token = login['token']
kb = world['knowledge_bases']['acme-eng']['id']

def snapshot():
    result = {}
    page = 1
    while True:
        data = request('GET', f'/memories?kb_id={kb}&page={page}&page_size=100', token=token)['data']
        for row in data['items']:
            m = row['memory']
            result[m['id']] = {key: m[key] for key in ('content', 'verdict', 'importance', 'tier', 'kb_id', 'tenant_id')}
        if page * 100 >= data['total']:
            return result
        page += 1

before = snapshot()
result = request('POST', '/memories/relink', {'kb_id': kb}, token)['data']
assert result['reembedded'] == 3, result
assert snapshot() == before, 'backfill changed user-authored fields or scope'
print(f"PASS: cloned upgrade relink re-embedded {result['reembedded']} legacy rows with real models; {len(before)} memories preserved")
