#!/usr/bin/env python3
"""Public-API half of queue_smoke.sh; state contains IDs, never bearer tokens."""
import json
import os
import pathlib
import sys
import urllib.error
import urllib.request
import uuid

root = pathlib.Path(__file__).parent
world = json.loads((root / 'seed-output.json').read_text())
assert world['base_url'] == 'http://localhost:18080', 'only the isolated host stack is supported'
mode, path = sys.argv[1], pathlib.Path(sys.argv[2])

def request(method, resource, data=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    body = json.dumps(data).encode() if data is not None else None
    with urllib.request.urlopen(urllib.request.Request(world['base_url'] + '/api/v1' + resource, body, headers, method=method), timeout=120) as response:
        raw = response.read()
        return json.loads(raw) if raw else None

login = request('POST', '/auth/login', {'email': world['users']['alice']['email'], 'password': world['password']})
token = login['token']
kb = world['knowledge_bases']['acme-eng']['id']
if mode == 'before':
    status = request('GET', f'/memories/dream?kb_id={kb}', token=token)['data']
    assert status['scheduled'] is False, 'mutating workers must be paused for this step'
    content = 'Queue restart smoke: the glass harmonica exhibit pairs a meteorite fragment with an ivory astrolabe; its shipping crate is lined with felt. Reference ' + str(uuid.uuid4())
    saved = request('POST', '/memories', {'kb_id': kb, 'content': content}, token)['data']
    assert saved['created'], 'test input unexpectedly deduplicated; refusing to delete a preexisting memory'
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as file:
        json.dump({'id': saved['memory']['id'], 'kb': kb, 'content': content}, file)
    print('PASS: memory stored while graph worker is paused')
elif mode in ('after', 'cleanup'):
    if path.exists():
        state = json.loads(path.read_text())
        assert state['kb'] == kb
        try:
            memory = request('GET', '/memories/' + str(uuid.UUID(state['id'])), token=token)['data']
        except urllib.error.HTTPError as error:
            if mode != 'cleanup' or error.code != 404:
                raise
        else:
            assert memory['content'] == state['content'] and memory['content'].startswith('Queue restart smoke:')
            assert memory['kb_id'] == kb and 'embedding' not in memory
            request('DELETE', '/memories/' + state['id'], token=token)
        path.unlink()
        print('PASS: persisted test memory verified and cleaned up')
else:
    raise SystemExit('use before, after or cleanup')
