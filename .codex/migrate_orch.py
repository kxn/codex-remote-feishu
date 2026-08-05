import re, os

RENAME = {
    'lookupStringFromAny': 'xutil.Stringify',
    'lookupBoolFromAny': 'xutil.LookupBoolFromAny',
    'lookupIntFromAny': 'xutil.LookupIntFromAny',
    'cloneJSONValue': 'xutil.CloneJSONValue',
    'maxInt': 'xutil.MaxInt',
    'firstNonEmpty': 'xutil.FirstNonEmpty',
    'firstNonEmptySlice': 'xutil.FirstNonEmpty',
}

DEFS = {
    'internal/core/orchestrator/service_helpers.go': ['lookupStringFromAny','lookupBoolFromAny','lookupIntFromAny','firstNonEmpty'],
    'internal/core/orchestrator/service_request_mcp.go': ['cloneJSONValue'],
    'internal/core/orchestrator/service_surface_thread_selection.go': ['maxInt'],
    'internal/core/orchestrator/execprogress/reasoning.go': ['lookupIntFromAny'],
    'internal/core/orchestrator/execprogress/snapshot.go': ['mapsFromAny','lookupStringFromAny','firstNonEmpty','firstNonEmptySlice'],
}

def remove_func(content, name):
    pat = re.compile(r'^func\s+(?:\([^)]*\)\s+)?' + name + r'\(', re.M)
    m = pat.search(content)
    if not m:
        print(f'  !! def not found: {name}')
        return content
    start = m.start()
    brace = content.index('{', start)
    depth = 0
    j = brace
    while j < len(content):
        if content[j] == '{':
            depth += 1
        elif content[j] == '}':
            depth -= 1
            if depth == 0:
                break
        j += 1
    end = j + 1
    return content[:start] + content[end:]

for path, names in DEFS.items():
    content = open(path, encoding='utf-8').read()
    for name in names:
        content = remove_func(content, name)
    open(path, 'w', encoding='utf-8', newline='').write(content)
    print(f'deleted defs in {path}')

for dp, dn, fn in os.walk('internal/core/orchestrator'):
    for f in sorted(fn):
        if not f.endswith('.go'):
            continue
        path = os.path.join(dp, f)
        content = open(path, encoding='utf-8').read()
        changed = False
        for name, new in RENAME.items():
            content2 = re.sub(r'\b' + name + r'\b', new, content)
            if content2 != content:
                changed = True
                content = content2
        if changed:
            open(path, 'w', encoding='utf-8', newline='').write(content)
            print(f'replaced in {path}')
print('done')
