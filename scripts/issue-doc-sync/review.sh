#!/usr/bin/env bash
set -euo pipefail

REPO="${ISSUE_DOC_SYNC_REPO:-kxn/codex-remote-feishu}"
STATE_FILE="${ISSUE_DOC_SYNC_STATE_FILE:-.codex/state/issue-doc-sync/state.json}"

usage() {
  cat <<'EOF'
Usage:
  scripts/issue-doc-sync/review.sh plan
  scripts/issue-doc-sync/review.sh next
  scripts/issue-doc-sync/review.sh inspect [issue-number]
  scripts/issue-doc-sync/review.sh record [issue-number] --decision skip|merge|new-doc --reason TEXT [--target-doc PATH ...] [--force]

Notes:
  - Processing order is oldest closed issue first.
  - If issue-number is omitted for inspect/record, the oldest pending candidate is used.
  - record re-runs plan after saving so you can see the remaining queue immediately.
EOF
}

run_tool() {
  go run ./cmd/issue-doc-sync "$@"
}

resolve_issue_number() {
  if [[ $# -gt 0 && "$1" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "$1"
    return 0
  fi
  run_tool next --repo "$REPO" --state-file "$STATE_FILE" --format number
}

cmd="${1:-}"
if [[ -z "$cmd" ]]; then
  usage
  exit 1
fi
shift || true

case "$cmd" in
  plan)
    run_tool plan --repo "$REPO" --state-file "$STATE_FILE" --format text
    ;;
  next)
    run_tool next --repo "$REPO" --state-file "$STATE_FILE" --format text
    ;;
  inspect)
    issue_number="$(resolve_issue_number "${1:-}")"
    if [[ $# -gt 0 && "$1" =~ ^[0-9]+$ ]]; then
      shift
    fi
    run_tool inspect --repo "$REPO" --issue "$issue_number" --format markdown
    ;;
  record)
    issue_number="$(resolve_issue_number "${1:-}")"
    if [[ $# -gt 0 && "$1" =~ ^[0-9]+$ ]]; then
      shift
    fi
    run_tool record --repo "$REPO" --state-file "$STATE_FILE" --issue "$issue_number" "$@"
    printf '\n'
    run_tool plan --repo "$REPO" --state-file "$STATE_FILE" --format text
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    usage
    exit 1
    ;;
esac
