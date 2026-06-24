#!/usr/bin/env python3
"""
Complete Vietnamese translation generator for WeKnora frontend i18n.
Reads en-US.ts and generates vi-VN.ts with Vietnamese translations for ALL strings.
"""
import re
import json

INPUT = '/Volumes/WORK/SUPERMEO/Libs/WeKnora/frontend/src/i18n/locales/en-US.ts'
OUTPUT = '/Volumes/WORK/SUPERMEO/Libs/WeKnora/frontend/src/i18n/locales/vi-VN.ts'

# Load translation map from a JSON file
MAP_FILE = '/Volumes/WORK/SUPERMEO/Libs/WeKnora/frontend/src/i18n/locales/vi_map.json'

with open(MAP_FILE, 'r', encoding='utf-8') as f:
    T = json.load(f)

with open(INPUT, 'r', encoding='utf-8') as f:
    content = f.read()

lines = content.split('\n')
out = []

for line in lines:
    # Match single-quoted string values: key: 'value' or key: 'value',
    m = re.match(r"^(\s*\w+: )'(.*)'(,?)\s*$", line)
    if m:
        prefix, val, suffix = m.group(1), m.group(2), m.group(3)
        if val in T:
            out.append(f"{prefix}'{T[val]}'{suffix}")
            continue

    # Match double-quoted string values: key: "value" or key: "value",
    m = re.match(r'^(\s*\w+: )"(.*)"?(,?)\s*$', line)
    if m:
        prefix, val, suffix = m.group(1), m.group(2), m.group(3)
        if val in T:
            out.append(f'{prefix}"{T[val]}"{suffix}')
            continue

    # Pass through unchanged (structure, keys, comments, etc.)
    out.append(line)

result = '\n'.join(out)
with open(OUTPUT, 'w', encoding='utf-8') as f:
    f.write(result)

# Verify
with open(OUTPUT, 'r', encoding='utf-8') as f:
    out_lines = f.readlines()

count = sum(1 for l in out_lines if re.match(r'^  \w+: \{', l.rstrip()))
print(f"Generated {OUTPUT} ({len(out_lines)} lines, {count} top-level keys)")
