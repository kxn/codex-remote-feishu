import re

paths = [
    'internal/adapter/feishu/controller.go',
    'internal/adapter/feishu/gateway/support.go',
    'internal/adapter/feishu/preview/support.go',
    'internal/adapter/feishu/projector/support.go',
    'internal/app/codexupgrade/detect.go',
    'internal/app/cronruntime/helpers.go',
    'internal/app/daemon/app_helpers.go',
    'internal/app/daemon/codexoauthstate/state.go',
    'internal/app/daemon/surfaceresume/feishu.go',
    'internal/app/desktopsession/runtime.go',
    'internal/app/install/service.go',
    'internal/app/vscodeshim/app.go',
    'internal/app/wrapper/app_process.go',
    'internal/codexstate/turn_patch_rollout.go',
    'internal/core/jsonrpcutil/error.go',
]

def show(path, name):
    content = open(path, encoding='utf-8').read()
    m = re.search(r'^func\s+(?:\([^)]*\)\s+)?' + name + r'\(', content, re.M)
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
    body = content[start:j+1]
    print(f'===== {path} =====')
    print(body)
    print()

for p in paths:
    show(p, 'firstNonEmpty')
