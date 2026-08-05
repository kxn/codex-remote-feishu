import re, os

GENERIC = ['cloneMap','cloneJSONValue','mapsFromAny','lookupStringFromAny','lookupBoolFromAny',
           'lookupIntFromAny','compactJSON','firstNonEmptyString']
CLAUDE = ['claudeHomeDir','isInternalInteractionTool','toolUseSummary','claudeToolItemKind',
          'claudeToolMetadata','claudeDynamicToolSemanticKind','mergeClaudeWebToolMetadata',
          'mergeClaudeFileChangeMetadata','mergeClaudeFileChangeMetadataPayload',
          'claudeLookupBool','buildClaudeDelegatedTaskText']

CAMEL = {'cloneMap':'CloneMap','cloneJSONValue':'CloneJSONValue','mapsFromAny':'MapsFromAny',
         'lookupStringFromAny':'LookupStringFromAny','lookupBoolFromAny':'LookupBoolFromAny',
         'lookupIntFromAny':'LookupIntFromAny','compactJSON':'CompactJSON',
         'firstNonEmptyString':'FirstNonEmpty',
         'claudeHomeDir':'ClaudeHomeDir','isInternalInteractionTool':'IsInternalInteractionTool',
         'toolUseSummary':'ToolUseSummary','claudeToolItemKind':'ClaudeToolItemKind',
         'claudeToolMetadata':'ClaudeToolMetadata','claudeDynamicToolSemanticKind':'ClaudeDynamicToolSemanticKind',
         'mergeClaudeWebToolMetadata':'MergeClaudeWebToolMetadata',
         'mergeClaudeFileChangeMetadata':'MergeClaudeFileChangeMetadata',
         'mergeClaudeFileChangeMetadataPayload':'MergeClaudeFileChangeMetadataPayload',
         'claudeLookupBool':'ClaudeLookupBool','buildClaudeDelegatedTaskText':'BuildClaudeDelegatedTaskText'}

# files -> definitions to delete
DEFS = {
    'internal/claudesessionstore/helpers.go': GENERIC + CLAUDE,
    'internal/adapter/claude/helpers.go': ['claudeHomeDir','cloneMap','cloneJSONValue','mapsFromAny',
        'lookupStringFromAny','lookupBoolFromAny','lookupIntFromAny','compactJSON',
        'claudeToolItemKind','claudeToolMetadata','claudeDynamicToolSemanticKind',
        'mergeClaudeWebToolMetadata','buildClaudeDelegatedTaskText'],
    'internal/adapter/claude/commands.go': ['firstNonEmptyString'],
    'internal/adapter/claude/file_change.go': ['mergeClaudeFileChangeMetadata',
        'mergeClaudeFileChangeMetadataPayload','claudeLookupBool'],
    'internal/adapter/claude/translator.go': ['isInternalInteractionTool','toolUseSummary'],
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

def walk(pkg):
    out = []
    for dp, dn, fn in os.walk(pkg):
        for f in sorted(fn):
            if f.endswith('.go'):
                out.append(os.path.join(dp, f))
    return out

targets = DEFS.keys()
for path in targets:
    content = open(path, encoding='utf-8').read()
    for name in DEFS[path]:
        content = remove_func(content, name)
    open(path, 'w', encoding='utf-8', newline='').write(content)
    print(f'deleted defs in {path}')

# replace call sites in every go file of both packages
for pkg in ['internal/adapter/claude', 'internal/claudesessionstore']:
    for path in walk(pkg):
        content = open(path, encoding='utf-8').read()
        changed = False
        for name in GENERIC:
            new = 'xutil.' + CAMEL[name]
            content2 = re.sub(r'\b' + name + r'\b', new, content)
            if content2 != content:
                changed = True
                content = content2
        for name in CLAUDE:
            new = 'claudeutil.' + CAMEL[name]
            content2 = re.sub(r'\b' + name + r'\b', new, content)
            if content2 != content:
                changed = True
                content = content2
        if changed:
            open(path, 'w', encoding='utf-8', newline='').write(content)
            print(f'replaced in {path}')
print('done')
