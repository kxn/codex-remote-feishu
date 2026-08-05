import re, os

# per-file definition deletions, then replacement mapping
JOBS = [
    # (path, [defs to delete], {name: replacement})
    ('internal/adapter/feishu/controller.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/adapter/feishu/gateway/support.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/adapter/feishu/preview/support.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/adapter/feishu/projector/support.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/adapter/feishu/projector/select_static_pagination.go', ['maxInt'], {'maxInt': 'xutil.MaxInt'}),
    ('internal/adapter/feishu/gateway_inbound_quoted_inputs.go', ['boolPtr'], {'boolPtr': 'xutil.BoolValue'}),
    ('internal/adapter/feishu/projector_render_helpers_test.go', ['containsString'], {'containsString': 'xutil.ContainsString'}),
    ('internal/app/codexupgrade/detect.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/app/cronruntime/helpers.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/app/daemon/app_helpers.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/app/daemon/app_cron_test.go', ['boolPtr'], {'boolPtr': 'xutil.BoolPtr'}),
    ('internal/app/daemon/app_cron_surface_test.go', ['containsString'], {'containsString': 'xutil.ContainsString'}),
    ('internal/app/daemon/codexoauthstate/state.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/app/daemon/surfaceresume/feishu.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/app/desktopsession/runtime.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/app/install/service.go', ['firstNonEmpty', 'boolPtr'], {'firstNonEmpty': 'xutil.FirstNonEmpty', 'boolPtr': 'xutil.BoolPtr'}),
    ('internal/app/vscodeshim/app.go', [], {}),  # keep local: path-specific cleanNonEmpty variant
    ('internal/app/wrapper/app_process.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/app/wrapper/app_headless_test.go', ['containsString'], {'containsString': 'xutil.ContainsString'}),
    ('internal/codexstate/turn_patch_rollout.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/core/jsonrpcutil/error.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
    ('internal/config/configfile.go', ['boolPtr'], {'boolPtr': 'xutil.BoolPtr'}),
    ('internal/config/proxyenv.go', ['containsString'], {'containsString': 'xutil.ContainsString'}),
    ('internal/externalaccess/service.go', ['maxInt'], {'maxInt': 'xutil.MaxInt'}),
    ('testkit/mockclaude/mockclaude.go', ['firstNonEmpty'], {'firstNonEmpty': 'xutil.FirstNonEmpty'}),
]

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
        if content[j] == '{': depth += 1
        elif content[j] == '}':
            depth -= 1
            if depth == 0: break
        j += 1
    return content[:start] + content[j+1:]

# global rename map for call sites across all go files (except vscodeshim dir)
GLOBAL_RENAME = {
    'firstNonEmpty': 'xutil.FirstNonEmpty',
    'maxInt': 'xutil.MaxInt',
    'containsString': 'xutil.ContainsString',
}

for path, defs, renames in JOBS:
    if not os.path.exists(path):
        print(f'  !! missing file: {path}')
        continue
    content = open(path, encoding='utf-8').read()
    for name in defs:
        content = remove_func(content, name)
    for name, new in renames.items():
        content = re.sub(r'\b' + name + r'\b', new, content)
    open(path, 'w', encoding='utf-8', newline='').write(content)
    print(f'processed {path}')

# global call-site replacement across internal/ + testkit/, skipping vscodeshim
for root in ['internal', 'testkit']:
    for dp, dn, fn in os.walk(root):
        dn[:] = [d for d in dn if d != '.git']
        if 'vscodeshim' in dp:
            continue
        for f in sorted(fn):
            if not f.endswith('.go'):
                continue
            path = os.path.join(dp, f)
            content = open(path, encoding='utf-8').read()
            changed = False
            for name, new in GLOBAL_RENAME.items():
                content2 = re.sub(r'\b' + name + r'\b', new, content)
                if content2 != content:
                    changed = True
                    content = content2
            if changed:
                open(path, 'w', encoding='utf-8', newline='').write(content)
                print(f'replaced {path}')
print('done')
