#!/usr/bin/env python3
"""Compare scoped REST reads on the original and the restored isolated servers."""
import json
import sys
import urllib.parse
import urllib.request

original, restored, seed = sys.argv[1], sys.argv[2], sys.argv[3]
world = json.loads(open(seed).read())
assert original == 'http://localhost:18080' and restored.startswith('http://localhost:1808'), 'isolated servers only'
email = world['users']['alice']['email']
assert email.endswith('@memv2-e2e.test'), 'refusing a non-fixture account'
kb = world['knowledge_bases']['acme-eng']['id']


def call(base, path, token=None, data=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    body = json.dumps(data).encode() if data is not None else None
    with urllib.request.urlopen(urllib.request.Request(base + '/api/v1' + path, body, headers), timeout=60) as response:
        return json.loads(response.read())


def snapshot(base):
    token = call(base, '/auth/login', data={'email': email, 'password': world['password']})['token']
    listing = call(base, f'/memories?kb_id={kb}&page_size=100&verdicts=none,fixed,refuted,decision,gotcha,wip', token)
    data = listing.get('data', listing)
    items = data.get('items', data) if isinstance(data, dict) else data
    ids = sorted((item.get('memory') or item)['id'] for item in items)
    results = {}
    for query in ('PDF font rendering', 'Node npm version', 'triển khai'):
        found = call(base, '/memories/search?' + urllib.parse.urlencode({'kb_id': kb, 'q': query, 'limit': 5}), token)
        rows = found.get('data', found)
        rows = rows.get('items', rows.get('results', rows)) if isinstance(rows, dict) else rows
        results[query] = [(row.get('memory') or row).get('id') for row in rows]
    settings = call(base, f'/memories/settings?kb_id={kb}', token)['data']
    health = call(base, f'/memories/health?kb_id={kb}', token)['data']
    return {'ids': ids, 'search': results, 'settings': [settings['dreamer_model_id'], settings['text_model_id']],
            'health_total': health.get('coverage', {}).get('total')}


a, b = snapshot(original), snapshot(restored)
assert a == b, f'restored server differs: {json.dumps(a)[:400]} vs {json.dumps(b)[:400]}'
print(f"PASS: restored REST matches original ({len(a['ids'])} acme-eng rows, {len(a['search'])} scoped searches, health coverage {a['health_total']})")
