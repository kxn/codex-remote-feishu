#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT_PATH="${ROOT_DIR}/scripts/check/release-readiness.sh"
work_dir="$(mktemp -d)"

cleanup() {
  rm -rf "${work_dir}"
}
trap cleanup EXIT

fake_gh="${work_dir}/gh"

cat > "${fake_gh}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

scenario="${FAKE_SCENARIO:-ready}"
endpoint="${2:-}"

if [[ "${1:-}" != "api" ]]; then
  echo "unexpected fake gh call: $*" >&2
  exit 1
fi

if [[ "${endpoint}" == "repos/kxn/codex-remote-feishu" ]]; then
    cat <<'JSON'
{"default_branch":"master"}
JSON
elif [[ "${scenario}" == "ready" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues/200" ]]; then
    cat <<'JSON'
{"number":200,"state":"closed","milestone":{"number":7,"title":"v0.14.0"},"labels":[{"name":"release:tracker"}],"body":"### 版本号\n\nv0.14.0\n\n### 发布轨道\n\nproduction\n\n### 发布前检查\n\n- [x] 同 Milestone 下未标记 `release:stretch` 的 issue 都已关闭\n- [x] `go test ./...` 已通过\n- [x] 安装 / 升级 / 关键路径验证已完成\n- [x] Release notes、目标版本号和发布轨道已确认\n"}
JSON
elif [[ "${scenario}" == "ready" && "${endpoint}" == "repos/kxn/codex-remote-feishu/branches/master" ]]; then
    cat <<'JSON'
{"name":"master"}
JSON
elif [[ "${scenario}" == "release_branch" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues/200" ]]; then
    cat <<'JSON'
{"number":200,"state":"closed","milestone":{"number":7,"title":"v0.14.0"},"labels":[{"name":"release:tracker"}],"body":"### 版本号\n\nv0.14.0\n\n### 发布轨道\n\nproduction\n\n### 发布分支\n\nrelease/1.5\n\n### 发布前检查\n\n- [x] 同 Milestone 下未标记 `release:stretch` 的 issue 都已关闭\n- [x] `go test ./...` 已通过\n- [x] 安装 / 升级 / 关键路径验证已完成\n- [x] Release notes、目标版本号和发布轨道已确认\n"}
JSON
elif [[ "${scenario}" == "release_branch" && "${endpoint}" == "repos/kxn/codex-remote-feishu/branches/release%2F1.5" ]]; then
    cat <<'JSON'
{"name":"release/1.5"}
JSON
elif [[ "${scenario}" == "release_branch" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues?state=open&milestone=7&per_page=100" ]]; then
    cat <<'JSON'
[{"number":201,"title":"Optional polish","labels":[{"name":"release:stretch"}]}]
JSON
elif [[ "${scenario}" == "ready" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues?state=open&milestone=7&per_page=100" ]]; then
    cat <<'JSON'
[{"number":201,"title":"Optional polish","labels":[{"name":"release:stretch"}]}]
JSON
elif [[ "${scenario}" == "blocker" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues/200" ]]; then
    cat <<'JSON'
{"number":200,"state":"closed","milestone":{"number":7,"title":"v0.14.0"},"labels":[{"name":"release:tracker"}],"body":"### 版本号\n\nv0.14.0\n\n### 发布轨道\n\nproduction\n\n### 发布前检查\n\n- [x] 同 Milestone 下未标记 `release:stretch` 的 issue 都已关闭\n- [x] `go test ./...` 已通过\n- [x] 安装 / 升级 / 关键路径验证已完成\n- [x] Release notes、目标版本号和发布轨道已确认\n"}
JSON
elif [[ "${scenario}" == "blocker" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues?state=open&milestone=7&per_page=100" ]]; then
    cat <<'JSON'
[{"number":202,"title":"Blocking fix","labels":[{"name":"bug"}]}]
JSON
elif [[ "${scenario}" == "blocker" && "${endpoint}" == "repos/kxn/codex-remote-feishu/branches/master" ]]; then
    cat <<'JSON'
{"name":"master"}
JSON
elif [[ "${scenario}" == "unchecked" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues/200" ]]; then
    cat <<'JSON'
{"number":200,"state":"closed","milestone":{"number":7,"title":"v0.14.0"},"labels":[{"name":"release:tracker"}],"body":"### 版本号\n\nv0.14.0\n\n### 发布轨道\n\nproduction\n\n### 发布前检查\n\n- [ ] 同 Milestone 下未标记 `release:stretch` 的 issue 都已关闭\n- [x] `go test ./...` 已通过\n- [x] 安装 / 升级 / 关键路径验证已完成\n- [x] Release notes、目标版本号和发布轨道已确认\n"}
JSON
elif [[ "${scenario}" == "unchecked" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues?state=open&milestone=7&per_page=100" ]]; then
    cat <<'JSON'
[]
JSON
elif [[ "${scenario}" == "unchecked" && "${endpoint}" == "repos/kxn/codex-remote-feishu/branches/master" ]]; then
    cat <<'JSON'
{"name":"master"}
JSON
elif [[ "${scenario}" == "mismatch" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues/200" ]]; then
    cat <<'JSON'
{"number":200,"state":"closed","milestone":{"number":7,"title":"v0.14.1"},"labels":[{"name":"release:tracker"}],"body":"### 版本号\n\nv0.14.0\n\n### 发布轨道\n\nproduction\n\n### 发布前检查\n\n- [x] 同 Milestone 下未标记 `release:stretch` 的 issue 都已关闭\n- [x] `go test ./...` 已通过\n- [x] 安装 / 升级 / 关键路径验证已完成\n- [x] Release notes、目标版本号和发布轨道已确认\n"}
JSON
elif [[ "${scenario}" == "mismatch" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues?state=open&milestone=7&per_page=100" ]]; then
    cat <<'JSON'
[]
JSON
elif [[ "${scenario}" == "mismatch" && "${endpoint}" == "repos/kxn/codex-remote-feishu/branches/master" ]]; then
    cat <<'JSON'
{"name":"master"}
JSON
elif [[ "${scenario}" == "missing_branch" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues/200" ]]; then
    cat <<'JSON'
{"number":200,"state":"closed","milestone":{"number":7,"title":"v0.14.0"},"labels":[{"name":"release:tracker"}],"body":"### 版本号\n\nv0.14.0\n\n### 发布轨道\n\nproduction\n\n### 发布分支\n\nrelease/9.9\n\n### 发布前检查\n\n- [x] 同 Milestone 下未标记 `release:stretch` 的 issue 都已关闭\n- [x] `go test ./...` 已通过\n- [x] 安装 / 升级 / 关键路径验证已完成\n- [x] Release notes、目标版本号和发布轨道已确认\n"}
JSON
elif [[ "${scenario}" == "missing_branch" && "${endpoint}" == "repos/kxn/codex-remote-feishu/issues?state=open&milestone=7&per_page=100" ]]; then
    cat <<'JSON'
[]
JSON
else
  echo "unexpected fake gh endpoint for scenario ${scenario}: ${endpoint}" >&2
  exit 1
fi
EOF

chmod +x "${fake_gh}"

expect_success() {
  local scenario="$1"
  local expected_ref="$2"
  local output_file="${work_dir}/output-${scenario}.txt"
  FAKE_SCENARIO="${scenario}" GH_BIN="${fake_gh}" REPOSITORY="kxn/codex-remote-feishu" GITHUB_OUTPUT="${output_file}" \
    bash "${SCRIPT_PATH}" --issue 200 --require-closed >/dev/null
  grep -Fx "ref=${expected_ref}" "${output_file}" >/dev/null
  grep -Fx "track=production" "${output_file}" >/dev/null
  grep -Fx "version=v0.14.0" "${output_file}" >/dev/null
  grep -Fx "milestone=v0.14.0" "${output_file}" >/dev/null
}

expect_failure() {
  local scenario="$1"
  local pattern="$2"
  local log_file="${work_dir}/${scenario}.log"
  if FAKE_SCENARIO="${scenario}" GH_BIN="${fake_gh}" REPOSITORY="kxn/codex-remote-feishu" \
    bash "${SCRIPT_PATH}" --issue 200 --require-closed >"${log_file}" 2>&1; then
    echo "expected scenario ${scenario} to fail" >&2
    cat "${log_file}" >&2
    exit 1
  fi
  grep -F "${pattern}" "${log_file}" >/dev/null
}

expect_success ready master
expect_success release_branch release/1.5
expect_failure blocker "blocking milestone issues remain"
expect_failure unchecked "unchecked 发布前检查 items"
expect_failure mismatch "does not match tracker version"
expect_failure missing_branch "does not exist in repo"

echo "release readiness selftest: ok"
