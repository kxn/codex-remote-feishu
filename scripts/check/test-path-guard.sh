#!/usr/bin/env bash
# test-path-guard.sh — Detect cross-platform path comparison pitfalls in Go tests.
#
# Background: On macOS /var → /private/var (symlink); on Windows 8.3 short
# names (RUNNER~1) differ from long names (runneradmin). String equality on
# paths fails on CI but passes locally.
#
# This script checks for the TWO patterns that have caused real CI failures.
# It intentionally does NOT flag:
#   - /tmp/... or /data/dl/... in JSON fixtures / mock data (just strings)
#   - Path comparisons using filepath.Join (both sides go through same construction)
#   - Path comparisons against constants (not t.TempDir-derived)
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

violations=0

# Pattern 1: Workspace/ThreadCWD/WorkDir assigned from t.TempDir() without
# normalization. The daemon runs these through filepath.Clean which resolves
# on macOS (/var → /private/var) and Windows (RUNNER~1 → runneradmin),
# creating a mismatch with the raw t.TempDir() value.
#
# Fix: workspaceDir := evalSymlinkForTest(t, t.TempDir())
echo "=== [1] Unnormalized t.TempDir() as workspace/CWD path ==="
while IFS= read -r line; do
  case "${line}" in
    *EvalSymlinks*|*evalSymlink*|*filepath.Clean*) continue ;;
  esac
  echo "  ${line}"
  violations=$((violations + 1))
done < <(
  grep -rn --include='*_test.go' \
    -E '(WorkspaceRoot|WorkspaceKey|ThreadCWD|WorkDir):\s*t\.TempDir\(\)' \
    --exclude-dir='.git' --exclude-dir='vendor' \
    . 2>/dev/null || true
)

# Pattern 2: Daemon tests that create instances with hardcoded /data/dl/
# workspace paths AND trigger real headless starts (not mocked).
# This is the exact pattern that caused repeated CI failures — the headless
# start fails because the directory doesn't exist on CI, or the path
# comparison fails due to platform differences.
#
# Only flag daemon/ tests — orchestrator/ tests mock startHeadless and
# never actually fork/exec, so they're safe.
#
# Fix: use t.TempDir() for workspace paths in tests that trigger headless starts.
echo ""
echo "=== [2] Daemon tests with hardcoded workspace + headless trigger ==="
while IFS= read -r file; do
  # Only check daemon/ tests (orchestrator mocks startHeadless)
  case "${file}" in
    *internal/app/daemon/*_test.go) ;;
    *) continue ;;
  esac
  if grep -qE 'Workspace(Root|Key):\s*"/data/dl/' "${file}" 2>/dev/null && \
     grep -qE 'startHeadless|DaemonCommandStartHeadless|HandleAction.*ActionUseThread|ActionAttachInstance' "${file}" 2>/dev/null; then
    count=$(grep -cE 'Workspace(Root|Key):\s*"/data/dl/' "${file}" 2>/dev/null || echo 0)
    echo "  ${file}: ${count} hardcoded workspace path(s) with headless trigger"
    violations=$((violations + 1))
  fi
done < <(find . -name '*_test.go' -not -path './.git/*' -not -path './vendor/*' 2>/dev/null)

echo ""
if [ "${violations}" -gt 0 ]; then
  echo "FAIL: found ${violations} potential cross-platform path issue(s)."
  echo ""
  echo "Fix patterns:"
  echo "  [1] WorkspaceRoot/Key/ThreadCWD from t.TempDir():"
  echo "      workspaceDir := evalSymlinkForTest(t, t.TempDir())"
  echo "  [2] Hardcoded /data/dl/ in tests with headless triggers:"
  echo "      replace with t.TempDir() or evalSymlinkForTest(t, t.TempDir())"
  echo ""
  echo "Helpers: internal/app/daemon/app_headless_test_helpers_test.go"
  exit 1
fi

echo "PASS: no cross-platform path issues detected."
exit 0
