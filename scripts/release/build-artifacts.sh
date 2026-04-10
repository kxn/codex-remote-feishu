#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

version="${1:-}"
output_dir="${2:-dist}"

if [[ -z "${version}" ]]; then
  echo "usage: $0 <version> [output_dir]" >&2
  exit 1
fi

bash "${ROOT_DIR}/scripts/web/build-admin-ui.sh"

rm -rf "${output_dir}"
mkdir -p "${output_dir}"

platforms=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
)

archives=()

for platform in "${platforms[@]}"; do
  read -r goos goarch <<<"${platform}"
  package_name="codex-remote-feishu_${version#v}_${goos}_${goarch}"
  staging_dir="${output_dir}/${package_name}"
  mkdir -p "${staging_dir}"

  extension=""
  if [[ "${goos}" == "windows" ]]; then
    extension=".exe"
  fi

  bash "${ROOT_DIR}/scripts/externalaccess/prepare-cloudflared-embed.sh" "${goos}" "${goarch}"

  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
    go build -trimpath -ldflags "-X main.version=${version}" \
    -o "${staging_dir}/codex-remote${extension}" ./cmd/codex-remote

  cp README.md QUICKSTART.md CHANGELOG.md "${staging_dir}/"
  cp -R deploy "${staging_dir}/"

  if [[ "${goos}" == "windows" ]]; then
    archive_path="${output_dir}/${package_name}.zip"
    (
      cd "${output_dir}"
      zip -qr "$(basename "${archive_path}")" "$(basename "${staging_dir}")"
    )
  else
    archive_path="${output_dir}/${package_name}.tar.gz"
    tar -C "${output_dir}" -czf "${archive_path}" "$(basename "${staging_dir}")"
  fi

  archives+=("${archive_path}")
  rm -rf "${staging_dir}"
done

cp install-release.sh "${output_dir}/codex-remote-feishu-install.sh"

checksum_cmd="sha256sum"
if ! command -v "${checksum_cmd}" >/dev/null 2>&1; then
  checksum_cmd="shasum -a 256"
fi

(
  cd "${output_dir}"
  ${checksum_cmd} ./*.tar.gz ./*.zip ./*.sh > checksums.txt
)
