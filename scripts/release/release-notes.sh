#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

version="${VERSION:-}"
latest_tag="${LATEST_TAG:-}"
track="${RELEASE_TRACK:-}"

if [[ -z "${version}" ]]; then
  echo "VERSION is required." >&2
  exit 1
fi

if [[ -z "${track}" ]]; then
  case "${version}" in
    *-alpha.*) track="alpha" ;;
    *-beta.*) track="beta" ;;
    *) track="production" ;;
  esac
fi

if [[ -n "${latest_tag}" ]]; then
  mapfile -t commits < <(git log --reverse --format='%h%x09%s' "${latest_tag}..HEAD")
else
  mapfile -t commits < <(git log --reverse --format='%h%x09%s')
fi

changelog_section=""
if [[ -f CHANGELOG.md ]]; then
  changelog_section="$(
    awk -v version="${version}" '
      $0 ~ "^##[[:space:]]+" version "([[:space:]]|\\(|$)" {
        in_section=1
        next
      }
      in_section && $0 ~ "^##[[:space:]]+" {
        exit
      }
      in_section {
        print
      }
    ' CHANGELOG.md | sed '/./,$!d'
  )"
fi

breaking=()
features=()
fixes=()
maintenance=()

for line in "${commits[@]}"; do
  hash="${line%%$'\t'*}"
  subject="${line#*$'\t'}"
  item="- ${subject} (${hash})"

  if [[ "${subject}" =~ BREAKING[[:space:]]CHANGE ]] || [[ "${subject}" =~ ^[^[:space:]]+(\(.+\))?!: ]]; then
    breaking+=("${item}")
  elif [[ "${subject}" =~ ^feat(\(.+\))?: ]] || [[ "${subject}" =~ ^(Add|Implement|Create|Support)[[:space:]] ]]; then
    features+=("${item}")
  elif [[ "${subject}" =~ ^(fix(\(.+\))?:|Fix|Handle|Correct)[[:space:]] ]]; then
    fixes+=("${item}")
  else
    maintenance+=("${item}")
  fi
done

print_section() {
  local title="$1"
  shift
  if [[ "$#" -eq 0 ]]; then
    return
  fi
  echo "### ${title}"
  printf '%s\n' "$@"
  echo
}

echo "版本：${version}"
echo
echo "发布路线：${track}"
echo
if [[ -n "${latest_tag}" ]]; then
  echo "变更范围：自 ${latest_tag} 以来。"
else
  echo "这是首个版本。"
fi
echo
if [[ -n "${changelog_section//[[:space:]]/}" ]]; then
  echo "## 本次重点"
  echo
  printf '%s\n' "${changelog_section}"
  echo
fi
echo "## 安装与升级"
echo
echo "默认安装最新正式版："
echo
echo '```bash'
echo "curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash"
echo '```'
echo
if [[ "${track}" != "production" ]]; then
  echo "如果你想提前体验最新 ${track} 版本："
  echo
  echo '```bash'
  echo "curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track ${track}"
  echo '```'
  echo
fi
echo "安装脚本会下载 GitHub 构建好的 release 包，安装二进制，启动本地后台服务，并打开或打印 WebSetup 地址。"
echo
echo "固定到这个版本："
echo
echo '```bash'
echo "curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version ${version}"
echo '```'
echo
echo "手动解压归档安装："
echo
echo '```bash'
echo "./codex-remote install -bootstrap-only -start-daemon"
echo '```'
echo
echo "已经完成接入的用户，后续升级统一在飞书里发送："
echo
echo '```text'
echo "/upgrade latest"
echo '```'
echo
echo "如果你默认安装的是正式版，这条命令会继续更新到最新正式版；如果你之前装的是 beta 或 alpha，它会继续沿着当前已安装的更新路线往前走。"
echo
echo "## 详细变更"
echo
print_section "不兼容变更" "${breaking[@]}"
print_section "功能" "${features[@]}"
print_section "修复" "${fixes[@]}"
print_section "维护" "${maintenance[@]}"
