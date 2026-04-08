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

echo "Release ${version}"
echo
echo "Track: ${track}."
echo
if [[ -n "${latest_tag}" ]]; then
  echo "Changes since ${latest_tag}."
else
  echo "Initial release."
fi
echo
if [[ -n "${changelog_section//[[:space:]]/}" ]]; then
  echo "## Highlights"
  echo
  printf '%s\n' "${changelog_section}"
  echo
fi
echo "## Install"
echo
echo "Latest production install:"
echo
echo '```bash'
echo "curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash"
echo '```'
echo
if [[ "${track}" != "production" ]]; then
  echo "Latest ${track} install:"
  echo
  echo '```bash'
  echo "curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track ${track}"
  echo '```'
  echo
fi
echo "The installer downloads the GitHub-built release archive, installs the binary, starts the local daemon, and opens or prints the WebSetup URL."
echo
echo "Pin this version:"
echo
echo '```bash'
echo "curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version ${version}"
echo '```'
echo
echo "Manual archive install:"
echo
echo '```bash'
echo "./codex-remote install -bootstrap-only -start-daemon"
echo '```'
echo
echo "## Detailed Changes"
echo
print_section "Breaking Changes" "${breaking[@]}"
print_section "Features" "${features[@]}"
print_section "Fixes" "${fixes[@]}"
print_section "Maintenance" "${maintenance[@]}"
