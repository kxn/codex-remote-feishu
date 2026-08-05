import re, os

targets = {
    'firstNonEmpty': None,
    'maxInt': None,
    'minInt': None,
    'clampInt': None,
    'containsString': None,
    'ptr': None,
    'lookupStringFromAny': None,
    'lookupIntFromAny': None,
    'lookupBoolFromAny': None,
    'cloneJSONValue': None,
    'cloneMap': None,
    'mapsFromAny': None,
    'compactJSON': None,
}

roots = ['internal']
skip = {'.git', 'web', 'testkit', 'scripts', '.codex'}

def walk():
    for root in roots:
        for dp, dn, fn in os.walk(root):
            dn[:] = [d for d in dn if d not in skip]
            for f in fn:
                if f.endswith('.go'):
                    yield os.path.join(dp, f)

pat = re.compile(r'^func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\(', re.M)
seen = {}
for path in walk():
    try:
        content = open(path, encoding='utf-8', errors='replace').read()
    except Exception:
        continue
    for m in pat.finditer(content):
        name = m.group(1)
        if name in targets and name not in seen:
            # extract signature line
            line_start = content[:m.start()].count('\n')
            lines = content.splitlines()
            sig = lines[line_start].strip()
            # gather until '(' balanced
            depth = 0
            i = line_start
            full = sig
            while i < len(lines):
                depth += lines[i].count('(') - lines[i].count(')')
                if depth <= 0 and i > line_start:
                    break
                i += 1
                if i < len(lines) and depth > 0:
                    full += ' ' + lines[i].strip()
            seen[name] = (path, line_start+1, full)

for name in targets:
    if name in seen:
        p, ln, sig = seen[name]
        print(f'{name}: {sig}   @ {p}:{ln}')
    else:
        print(f'{name}: NOT FOUND')
