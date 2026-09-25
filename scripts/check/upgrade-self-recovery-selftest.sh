#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source <(sed -n '/^wait_for_admin_recovery() {/,/^}/p' "${ROOT_DIR}/upgrade-self.sh")

test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT
installed="${test_dir}/installed binary"
printf 'new artifact\n' > "${installed}"
expected="$(shasum -a 256 "${installed}" | awk '{print $1}')"
setup_required=false

# Stub only HTTP: no service, credentials, or installed executable is touched.
curl() {
  case "${*: -1}" in
    */api/admin/bootstrap-state) printf '{"setupRequired":%s}\n' "${setup_required}" ;;
    *) printf 'ok\n' ;;
  esac
}

printf 'old artifact\n' > "${installed}"
if wait_for_admin_recovery http://test.invalid 1 "${expected}" "${installed}"; then
  echo 'healthy old binary was incorrectly accepted' >&2
  exit 1
fi

printf 'new artifact\n' > "${installed}"
wait_for_admin_recovery http://test.invalid 2 "${expected}" "${installed}"

setup_required=true
if wait_for_admin_recovery http://test.invalid 1 "${expected}" "${installed}"; then
  echo 'setup-required instance was incorrectly accepted' >&2
  exit 1
fi

setup_required=false
rm "${installed}"
if wait_for_admin_recovery http://test.invalid 1 "${expected}" "${installed}"; then
  echo 'missing binary was incorrectly accepted' >&2
  exit 1
fi

echo 'upgrade-self recovery selftest: passed'
