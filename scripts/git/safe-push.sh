#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REMOTE=""
BRANCH=""
TEST_CMD="${SAFE_PUSH_TEST_CMD:-go test ./...}"
NO_TEST=0
ALWAYS_TEST=0
CONFIRM_REBASE_REVIEW=0

usage() {
  cat <<'EOF'
usage: ./safe-push.sh [--remote <name>] [--branch <name>] [--test-cmd <cmd>] [--always-test] [--no-test] [--confirm-rebase-review]

Safe happy-path push helper for this repository.

Default behavior:
  1. require a clean worktree
  2. verify lightweight CI guardrails before any remote sync
  3. check whether the target remote branch exists and fetch it if present
  4. if upstream moved ahead, rebase onto it
  5. rerun tests only when a rebase actually happened
  6. if a rebase happened, require a post-rebase review confirmation
  7. push only if every previous step succeeds, and set upstream for a new branch

It intentionally does not try to auto-resolve conflicts or recover from test
failures. In those cases it stops and leaves the repository state visible for
manual handling.

It also verifies repository Go formatting plus lightweight CI guardrails before
any fetch/rebase/push work so missing local git hooks cannot leak common fast
failures into CI.

options:
  --remote <name>    remote to push/fetch (default: tracking remote, else origin)
  --branch <name>    branch to push/rebase against (default: tracking branch, else current branch)
  --test-cmd <cmd>   shell command to run after successful rebase (default: `go test ./...`)
  --always-test      run the test command even when no rebase was needed
  --no-test          skip post-rebase tests entirely
  --confirm-rebase-review
                     confirm post-rebase review in non-interactive mode
  -h, --help         show this help text
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --remote)
      [[ $# -ge 2 ]] || { echo "missing value for --remote" >&2; exit 1; }
      REMOTE="$2"
      shift 2
      ;;
    --branch)
      [[ $# -ge 2 ]] || { echo "missing value for --branch" >&2; exit 1; }
      BRANCH="$2"
      shift 2
      ;;
    --test-cmd)
      [[ $# -ge 2 ]] || { echo "missing value for --test-cmd" >&2; exit 1; }
      TEST_CMD="$2"
      shift 2
      ;;
    --always-test)
      ALWAYS_TEST=1
      shift
      ;;
    --no-test)
      NO_TEST=1
      shift
      ;;
    --confirm-rebase-review)
      CONFIRM_REBASE_REVIEW=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

cd "${ROOT_DIR}"

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "not inside a git worktree: ${ROOT_DIR}" >&2
  exit 1
fi

if ! git diff --quiet --ignore-submodules -- || ! git diff --cached --quiet --ignore-submodules --; then
  echo "working tree has uncommitted changes; commit or stash them before safe-push" >&2
  exit 1
fi

if [[ -n "$(git ls-files --others --exclude-standard)" ]]; then
  echo "working tree has untracked files; add, ignore, or move them before safe-push" >&2
  exit 1
fi

validation_stamp="$(git rev-parse --git-path codex-remote/pre-commit-tree)"
head_tree="$(git rev-parse 'HEAD^{tree}')"
validated_tree=""
if [[ -f "${validation_stamp}" ]]; then
  validated_tree="$(tr -d '[:space:]' < "${validation_stamp}")"
fi

if [[ -n "${validated_tree}" && "${validated_tree}" == "${head_tree}" ]]; then
  printf '[0/8] pre-commit guardrails already passed for HEAD tree %s\n' "${head_tree}"
  printf '[1/8] skip machine-local path check (validated by pre-commit)\n'
  printf '[2/8] skip legacy name check (validated by pre-commit)\n'
  printf '[3/8] skip Feishu broker guardrail (validated by pre-commit)\n'
else
  printf '[0/8] verify gofmt\n'
  bash scripts/check/go-format.sh

  printf '[1/8] check public docs for machine-local paths\n'
  bash scripts/check/no-local-paths.sh

  printf '[2/8] check legacy names are gone\n'
  bash scripts/check/no-legacy-names.sh

  printf '[3/8] check Feishu broker guardrail\n'
  bash scripts/check/feishu-call-broker.sh
fi

current_branch="$(git branch --show-current)"
if [[ -z "${current_branch}" ]]; then
  echo "detached HEAD is not supported; pass --branch explicitly or push manually" >&2
  exit 1
fi

upstream_ref="$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)"
if [[ -n "${upstream_ref}" ]]; then
  upstream_remote="${upstream_ref%%/*}"
  upstream_branch="${upstream_ref#*/}"
else
  upstream_remote=""
  upstream_branch=""
fi

if [[ -z "${REMOTE}" ]]; then
  REMOTE="${upstream_remote:-origin}"
fi
if [[ -z "${BRANCH}" ]]; then
  BRANCH="${upstream_branch:-${current_branch}}"
fi

branch_ref="refs/heads/${BRANCH}"
remote_ref="refs/remotes/${REMOTE}/${BRANCH}"
git check-ref-format "${branch_ref}"
new_branch=0
rebase_happened=0

printf '[4/8] check %s %s\n' "${REMOTE}" "${branch_ref}"
if remote_heads="$(git ls-remote --exit-code --heads -- "${REMOTE}" "${branch_ref}")"; then
  if ! awk -F '\t' -v ref="${branch_ref}" '$2 == ref { found = 1 } END { exit !found }' <<< "${remote_heads}"; then
    echo "remote branch probe did not return the exact ref: ${branch_ref}" >&2
    exit 1
  fi
else
  probe_status=$?
  if [[ "${probe_status}" != "2" ]]; then
    echo "could not check remote branch: ${REMOTE}/${BRANCH}" >&2
    exit "${probe_status}"
  fi
  new_branch=1
fi

if [[ "${new_branch}" == "1" ]]; then
  printf '[5/8] remote branch does not exist; skip fetch and rebase\n'
else
  printf '[4/8] fetch %s %s\n' "${REMOTE}" "${branch_ref}"
  git fetch "${REMOTE}" "${branch_ref}"

  if ! git show-ref --verify --quiet "${remote_ref}"; then
    echo "remote branch not found after fetch: ${remote_ref}" >&2
    exit 1
  fi

  counts="$(git rev-list --left-right --count "HEAD...${remote_ref}")"
  read -r ahead behind <<< "${counts}"

  if [[ "${behind}" != "0" ]]; then
    printf '[5/8] rebase onto %s/%s (ahead=%s behind=%s)\n' "${REMOTE}" "${BRANCH}" "${ahead}" "${behind}"
    if ! git rebase "${remote_ref}"; then
      cat >&2 <<'EOF'
rebase failed and was left in place for manual resolution.
resolve conflicts, continue or abort the rebase yourself, then rerun tests and push manually.
EOF
      exit 2
    fi
    rebase_happened=1
  else
    printf '[5/8] remote is not ahead (ahead=%s behind=%s); skip rebase\n' "${ahead}" "${behind}"
  fi
fi

if [[ "${NO_TEST}" == "1" ]]; then
  printf '[6/8] skip tests (--no-test)\n'
elif [[ "${rebase_happened}" == "1" || "${ALWAYS_TEST}" == "1" ]]; then
  printf '[6/8] run tests: %s\n' "${TEST_CMD}"
  if ! TEST_CMD="${TEST_CMD}" bash -lc 'unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy; eval "$TEST_CMD"'; then
    cat >&2 <<'EOF'
tests failed after repository sync.
fix the failure, rerun the relevant tests, then push manually or rerun safe-push.
EOF
    exit 3
  fi
else
  printf '[6/8] skip tests (no rebase happened)\n'
fi

if [[ "${rebase_happened}" == "1" ]]; then
  if [[ "${CONFIRM_REBASE_REVIEW}" == "1" ]]; then
    printf '[7/8] rebase review confirmed (--confirm-rebase-review)\n'
  elif [[ -t 0 ]]; then
    cat <<EOF
[7/8] rebase review required
the branch was rebased onto ${REMOTE}/${BRANCH}.
before pushing, re-audit:
  1. whether the implementation direction still matches the intended plan
  2. whether the final implementation still matches the intended behavior after the rebase

if deviations are found, fix them first, then continue pushing.
EOF
    read -r -p "review completed and deviations fixed if any? [y/N] " confirm
    case "${confirm}" in
      y|Y|yes|YES)
        printf '[7/8] rebase review confirmed interactively\n'
        ;;
      *)
        echo "post-rebase review not confirmed; push aborted" >&2
        exit 4
        ;;
    esac
  else
    cat >&2 <<EOF
[7/8] rebase review required (non-interactive shell)
the branch was rebased onto ${REMOTE}/${BRANCH}.
review direction and implementation drift first; fix drift if found.
then rerun with:
  ./safe-push.sh --confirm-rebase-review
EOF
    exit 4
  fi
elif [[ "${CONFIRM_REBASE_REVIEW}" == "1" ]]; then
  printf '[7/8] no rebase happened; --confirm-rebase-review ignored\n'
else
  printf '[7/8] no post-rebase review required\n'
fi

printf '[8/8] push %s HEAD:%s\n' "${REMOTE}" "${BRANCH}"
if [[ "${new_branch}" == "1" ]]; then
  git push --set-upstream --force-with-lease="${branch_ref}:" -- "${REMOTE}" "HEAD:${branch_ref}"
else
  git push "${REMOTE}" "HEAD:${branch_ref}"
fi
