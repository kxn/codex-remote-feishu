#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
probe_dir="$(mktemp -d)"
trap 'rm -rf "${probe_dir}"' EXIT

# Instrument the real host resolver only in a temporary Go overlay. Production
# builds do not expose a global debug hook or a switch that can disable IO.
python3 - "${repo_root}" "${probe_dir}" <<'PY'
import json
import pathlib
import sys

root, probe = map(pathlib.Path, sys.argv[1:])
source = root / "internal/core/state/workspace.go"
text = source.read_text()
entry = "func ResolveWorkspaceRootOnHost(value string) (string, error) {"
if text.count(entry) != 1:
    raise SystemExit("host resolver entry changed; update the navigation IO probe")
instrumented = """var TargetPickerHostResolutionForbidden bool
var TargetPickerHostResolutionReads int

""" + entry + """
    TargetPickerHostResolutionReads++
    if TargetPickerHostResolutionForbidden {
        panic("target picker navigation entered the host filesystem resolver")
    }
"""
replacement = probe / "workspace.go"
replacement.write_text(text.replace(entry, instrumented))
overlay = {
    "Replace": {
        str(source): str(replacement),
        str(root / "internal/core/orchestrator/target_picker_navigation_io_overlay_test.go"):
            str(root / "scripts/check/testdata/target_picker_navigation_io_test.go"),
    }
}
(probe / "overlay.json").write_text(json.dumps(overlay))
PY

cd "${repo_root}"
go test -overlay="${probe_dir}/overlay.json" ./internal/core/orchestrator \
  -run '^TestTargetPickerNavigationHostIOBoundary$' -count=1
