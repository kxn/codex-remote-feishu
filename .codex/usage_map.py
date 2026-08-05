import re, os

generic = ['cloneMap','cloneJSONValue','mapsFromAny','lookupStringFromAny','lookupBoolFromAny',
           'lookupIntFromAny','compactJSON','firstNonEmptyString']
claude_specific = ['claudeHomeDir','isInternalInteractionTool','toolUseSummary','claudeToolItemKind',
                   'claudeToolMetadata','claudeDynamicToolSemanticKind','mergeClaudeWebToolMetadata',
                   'mergeClaudeFileChangeMetadata','mergeClaudeFileChangeMetadataPayload',
                   'claudeLookupBool','buildClaudeDelegatedTaskText']
allnames = generic + claude_specific

def walk(pkg):
    out = []
    for dp, dn, fn in os.walk(pkg):
        for f in sorted(fn):
            if f.endswith('.go'):
                out.append(os.path.join(dp, f))
    return out

def func_bodies(content):
    """map func name -> set of names called inside (simple scan of body lines)"""
    lines = content.splitlines()
    bodies = {}
    cur = None
    depth = 0
    for i, line in enumerate(lines):
        if re.match(r'^func\s', line.strip()):
            m = re.match(r'^func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\(', line.strip())
            if m:
                cur = m.group(1)
                depth = line.count('{') - line.count('}')
                bodies[cur] = []
                continue
        if cur is not None:
            bodies[cur].append(line)
            depth += line.count('{') - line.count('}')
            if depth <= 0:
                cur = None
    return bodies

for pkg in ['internal/adapter/claude', 'internal/claudesessionstore']:
    print(f'########## {pkg} ##########')
    for path in walk(pkg):
        content = open(path, encoding='utf-8', errors='replace').read()
        uses = {}
        for name in allnames:
            cnt = len(re.findall(r'\b' + name + r'\b', content))
            defined = bool(re.search(r'^func\s+(?:\([^)]*\)\s+)?' + name + r'\(', content, re.M))
            refs = cnt - (1 if defined else 0)
            if refs > 0:
                uses[name] = refs
        if uses:
            rel = os.path.relpath(path, pkg)
            print(f'  {rel}: ' + ', '.join(f'{n}×{c}' for n, c in sorted(uses.items())))
    print()
