import re, os

names = ['firstNonEmpty','maxInt','minInt','boolPtr','containsString']

def walk():
    for dp, dn, fn in os.walk('internal'):
        dn[:] = [d for d in dn if d not in {'.git'}]
        for f in sorted(fn):
            if f.endswith('.go'):
                yield os.path.join(dp, f)

for name in names:
    print(f'=== {name} ===')
    for path in walk():
        content = open(path, encoding='utf-8', errors='replace').read()
        defined = bool(re.search(r'^func\s+(?:\([^)]*\)\s+)?' + name + r'\(', content, re.M))
        if defined:
            m = re.search(r'^func\s+(?:\([^)]*\)\s+)?' + name + r'\(', content, re.M)
            ln = content[:m.start()].count('\n') + 1
            print(f'  DEF {path}:{ln}')
