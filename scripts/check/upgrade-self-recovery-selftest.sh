#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source <(sed -n '/^binary_sha256() {/,/^}/p' "${ROOT_DIR}/upgrade-self.sh")
source <(sed -n '/^self_upgrade_artifact_committed() (/,/^)/p' "${ROOT_DIR}/upgrade-self.sh")
source <(sed -n '/^wait_for_admin_recovery() {/,/^}/p' "${ROOT_DIR}/upgrade-self.sh")

test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT
installed="${test_dir}/installed binary"
state_path="${test_dir}/install state.json"
SELF_TARGET_SCRIPT="${test_dir}/resolve target"
target_file="${test_dir}/target.env"
export target_file
cat > "${SELF_TARGET_SCRIPT}" <<'EOF'
#!/usr/bin/env bash
cat "${target_file}"
EOF
chmod +x "${SELF_TARGET_SCRIPT}"
write_target() {
  printf 'CODEX_REMOTE_SELF_TARGET_STATE_PATH=%q\n' "${state_path}"
  printf 'CODEX_REMOTE_SELF_TARGET_PENDING_UPGRADE_PHASE=%q\n' "$1"
  printf 'CODEX_REMOTE_SELF_TARGET_CURRENT_BINARY_PATH=%q\n' "$2"
}
write_target committed "${installed}" > "${target_file}"
printf 'new artifact\n' > "${installed}"
expected="$(binary_sha256 "${installed}")"
setup_required=false

# Stub only HTTP: no service, credentials, or installed executable is touched.
curl() {
  case "${*: -1}" in
    */api/admin/bootstrap-state) printf '{"setupRequired":%s}\n' "${setup_required}" ;;
    *) printf 'ok\n' ;;
  esac
}

printf 'old artifact\n' > "${installed}"
if wait_for_admin_recovery http://test.invalid 1 "${expected}" "${state_path}"; then
  echo 'healthy old binary was incorrectly accepted' >&2
  exit 1
fi

printf 'new artifact\n' > "${installed}"
wait_for_admin_recovery http://test.invalid 2 "${expected}" "${state_path}"

# A matching on-disk file does not prove the helper has completed observation.
for phase in prepared switching observing rolled_back failed; do
  write_target "${phase}" "${installed}" > "${target_file}"
  if self_upgrade_artifact_committed "${expected}" "${state_path}"; then
    echo "uncommitted ${phase} transaction was incorrectly accepted" >&2
    exit 1
  fi
done

# Legacy version-scoped installations migrate their live entry during upgrade.
legacy="${test_dir}/old version binary"
printf 'old artifact\n' > "${legacy}"
CODEX_REMOTE_SELF_TARGET_CURRENT_BINARY_PATH="${legacy}"
write_target committed "${installed}" > "${target_file}"
self_upgrade_artifact_committed "${expected}" "${state_path}"
[[ "${CODEX_REMOTE_SELF_TARGET_CURRENT_BINARY_PATH}" == "${legacy}" ]]
if self_upgrade_artifact_committed "${expected}" "${test_dir}/other state.json"; then
  echo 'another install target was incorrectly accepted' >&2
  exit 1
fi
if self_upgrade_artifact_committed '' "${state_path}"; then
  echo 'empty artifact fingerprint was incorrectly accepted' >&2
  exit 1
fi

setup_required=true
if wait_for_admin_recovery http://test.invalid 1 "${expected}" "${state_path}"; then
  echo 'setup-required instance was incorrectly accepted' >&2
  exit 1
fi

setup_required=false
rm "${installed}"
if wait_for_admin_recovery http://test.invalid 1 "${expected}" "${state_path}"; then
  echo 'missing binary was incorrectly accepted' >&2
  exit 1
fi

# Resolver/hash failures and malformed output must fail closed, never compare
# two empty strings as a successful installation.
printf '' > "${target_file}"
printf 'new artifact\n' > "${installed}"
CODEX_REMOTE_SELF_TARGET_STATE_PATH="${state_path}"
CODEX_REMOTE_SELF_TARGET_PENDING_UPGRADE_PHASE=committed
CODEX_REMOTE_SELF_TARGET_CURRENT_BINARY_PATH="${installed}"
if self_upgrade_artifact_committed "${expected}" "${state_path}"; then
  echo 'empty target resolver output was incorrectly accepted' >&2
  exit 1
fi
write_target committed "${legacy}" > "${target_file}"
binary_sha256() { return 1; }
if self_upgrade_artifact_committed "${expected}" "${state_path}"; then
  echo 'hash command failure was incorrectly accepted' >&2
  exit 1
fi
unset -f binary_sha256
binary_sha256() { return 0; }
if self_upgrade_artifact_committed "${expected}" "${state_path}"; then
  echo 'empty hash output was incorrectly accepted' >&2
  exit 1
fi
unset -f binary_sha256
source <(sed -n '/^binary_sha256() {/,/^}/p' "${ROOT_DIR}/upgrade-self.sh")

# Exercise each platform's tool without depending on both being installed.
# The primary old/new-file cases above use the real host tool.
hash_bin="${test_dir}/hash tools"
mkdir "${hash_bin}"
ln -s "$(command -v awk)" "${hash_bin}/awk"
export expected installed
cat > "${hash_bin}/shasum" <<'EOF'
#!/bin/bash
[[ "$#" == 3 && "$1" == -a && "$2" == 256 && "$3" == "${installed}" ]] || exit 1
printf '%s  %s\n' "${expected}" "${installed}"
EOF
chmod +x "${hash_bin}/shasum"
[[ "$(PATH="${hash_bin}" binary_sha256 "${installed}")" == "${expected}" ]]
cat > "${hash_bin}/sha256sum" <<'EOF'
#!/bin/bash
[[ "$#" == 1 && "$1" == "${installed}" ]] || exit 1
printf '%s  %s\n' "${expected}" "${installed}"
EOF
chmod +x "${hash_bin}/sha256sum"
[[ "$(PATH="${hash_bin}" binary_sha256 "${installed}")" == "${expected}" ]]
rm "${hash_bin}/shasum"
[[ "$(PATH="${hash_bin}" binary_sha256 "${installed}")" == "${expected}" ]]
rm "${hash_bin}/sha256sum"
if PATH="${hash_bin}" binary_sha256 "${installed}" 2>/dev/null; then
  echo 'missing hash tools were incorrectly accepted' >&2
  exit 1
fi

echo 'upgrade-self recovery selftest: passed'
