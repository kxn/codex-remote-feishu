import re, os

GENERIC = ['cloneMap','cloneJSONValue','lookupStringFromAny','lookupBoolFromAny',
           'lookupIntFromAny','firstNonEmptyString']

CAMEL = {'cloneMap':'CloneMap','cloneJSONValue':'CloneJSONValue',
         'lookupStringFromAny':'LookupStringFromAny','lookupBoolFromAny':'LookupBoolFromAny',
         'lookupIntFromAny':'LookupIntFromAny','firstNonEmptyString':'FirstNonEmpty'}

DEFS = {
    'internal/adapter/codex/translator_helpers.go': GENERIC,
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

for dp, dn, fn in os.walk('internal/adapter/codex'):
    for f in sorted(fn):
        if not f.endswith('.go'):
            continue
        path = os.path.join(dp, f)
        content = open(path, encoding='utf-8').read()
        changed = False
        for name in GENERIC:
            new = 'xutil.' + CAMEL[name]
            content2 = re.sub(r'\b' + name + r'\b', new, content)
            if content2 != content:
                changed = True
                content = content2
        if changed:
            open(path, 'w', encoding='utf-8', newline='').write(content)
            print(f'replaced in {path}')
print('done')
