#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/git/safe-push.sh"
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
git init -q --bare "${fixture}/origin.git"
git init -q -b codex/topic "${fixture}/work"
cd "${fixture}/work"
git config user.name Test
git config user.email test@example.invalid
mkdir -p scripts/git .git/codex-remote
cp "${script}" scripts/git/safe-push.sh
git add scripts
git commit -qm base
git remote add origin "${fixture}/origin.git"

safe_push() {
  git rev-parse 'HEAD^{tree}' > .git/codex-remote/pre-commit-tree
  bash scripts/git/safe-push.sh "$@"
}
remote_oid() {
  git --git-dir="${fixture}/origin.git" rev-parse "refs/heads/$1"
}
assert_missing() {
  ! git --git-dir="${fixture}/origin.git" show-ref --verify --quiet "refs/heads/$1"
}

safe_push
[[ "$(git rev-parse --abbrev-ref '@{upstream}')" == origin/codex/topic ]]
[[ "$(remote_oid codex/topic)" == "$(git rev-parse HEAD)" ]]
echo 'PASS first push sets upstream'

git commit -qm second --allow-empty
safe_push
[[ "$(remote_oid codex/topic)" == "$(git rev-parse HEAD)" ]]
echo 'PASS existing branch push'

git branch codex/competing-base HEAD~
if safe_push --branch codex/competing --always-test --test-cmd 'git push origin refs/heads/codex/competing-base:refs/heads/codex/competing'; then
  echo 'competing creation was accepted' >&2
  exit 1
fi
[[ "$(remote_oid codex/competing)" == "$(git rev-parse HEAD~)" ]]
echo 'PASS competing creation rejected even when a fast-forward was possible'

git remote set-url --push origin "${fixture}/origin.git"
git remote set-url origin "${fixture}/missing.git"
if safe_push --branch codex/probe-failed; then
  echo 'probe error was accepted' >&2
  exit 1
fi
assert_missing codex/probe-failed
echo 'PASS probe error prevented push despite usable push URL'
