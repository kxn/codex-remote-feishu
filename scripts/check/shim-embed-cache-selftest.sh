#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT
repo="${test_dir}/repo"
mkdir -p "${repo}/cmd/shim" "${repo}/scripts/shim" \
  "${repo}/internal/app/install" "${repo}/internal/transitive" \
  "${repo}/internal/windowsdep" "${repo}/internal/shim/embed"
cp "${ROOT_DIR}/scripts/shim/prepare-shim-embed.sh" "${repo}/scripts/shim/"
script="${repo}/scripts/shim/prepare-shim-embed.sh"
cat >"${repo}/go.mod" <<'EOF'
module example.test/shim-cache

go 1.24.6
EOF
cat >"${repo}/cmd/shim/main.go" <<'EOF'
package main
import "example.test/shim-cache/internal/app/install"
func main() { println(install.Message()) }
EOF
cat >"${repo}/internal/transitive/message.go" <<'EOF'
package transitive
const Message = "first"
EOF
cat >"${repo}/internal/app/install/message_linux.go" <<'EOF'
package install
import "example.test/shim-cache/internal/transitive"
func Message() string { return transitive.Message }
EOF
cat >"${repo}/internal/app/install/message_windows.go" <<'EOF'
package install
import "example.test/shim-cache/internal/windowsdep"
func Message() string { return windowsdep.Message }
EOF
cat >"${repo}/internal/windowsdep/message.go" <<'EOF'
package windowsdep
const Message = "first windows"
EOF

run_prepare() {
  bash "${script}" "$1" amd64 >"${test_dir}/prepare.log" 2>&1 || {
    tail -n 30 "${test_dir}/prepare.log" >&2
    return 1
  }
}
digest() {
  awk -F '"' '/SourceDigest:/{print $2}' "${repo}/internal/shim/embed/shim_$1_amd64_embed.go"
}
assert_hit() {
  run_prepare "$1"
  grep -Fq 'already up to date' "${test_dir}/prepare.log"
}
assert_rebuilt() {
  run_prepare "$1"
  if [[ "$(digest "$1")" == "$2" ]]; then
    echo "cache did not invalidate for $3" >&2
    exit 1
  fi
  grep -Fq 'prepared shim asset' "${test_dir}/prepare.log"
}

run_prepare linux
first="$(digest linux)"
[[ "${first}" =~ ^[[:xdigit:]]{64}$ ]]
assert_hit linux

# A production dependency below install was absent from the old manual list.
sed 's/"first"/"second"/' "${repo}/internal/transitive/message.go" >"${test_dir}/changed.go"
mv "${test_dir}/changed.go" "${repo}/internal/transitive/message.go"
assert_rebuilt linux "${first}" 'a transitive install dependency'

# Test-only files, unrelated files, and the generated outputs are not compiler
# inputs. Generated metadata must not create perpetual self-invalidations.
printf 'package transitive\nfunc ExampleMessage() {}\n' >"${repo}/internal/transitive/message_test.go"
printf 'unrelated\n' >"${repo}/README.md"
printf '\n// generated output comment\n' >>"${repo}/internal/shim/embed/shim_linux_amd64_embed.go"
assert_hit linux

before="$(digest linux)"
printf '\n// module metadata\n' >>"${repo}/go.mod"
assert_rebuilt linux "${before}" 'module metadata'
before="$(digest linux)"
GOFLAGS=-tags=shim_cache_fixture assert_rebuilt linux "${before}" 'Go build flags'
run_prepare linux

# Resolve the target graph, not the host graph: this package is Windows-only.
run_prepare windows
before="$(digest windows)"
printf 'package windowsdep\nconst Message = "second windows"\n' >"${repo}/internal/windowsdep/message.go"
assert_hit linux
assert_rebuilt windows "${before}" 'a Windows-only transitive dependency'

# The identities must survive relocating the checkout; no mtimes, cache paths,
# or absolute checkout paths may determine whether the helper is current.
run_prepare linux
cp -R "${repo}" "${test_dir}/relocated repo"
original_script="${script}"
script="${test_dir}/relocated repo/scripts/shim/prepare-shim-embed.sh"
assert_hit linux
script="${original_script}"

# Compression-tool and script changes also affect the generated artifact.
before="$(digest linux)"
real_zstd="$(command -v zstd)"
mkdir "${test_dir}/bin"
{
  printf '#!/usr/bin/env bash\n'
  printf 'if [[ "${1:-}" == --version ]]; then echo fixture-zstd-version; exit 0; fi\n'
  printf 'exec %q "$@"\n' "${real_zstd}"
} >"${test_dir}/bin/zstd"
chmod +x "${test_dir}/bin/zstd"
PATH="${test_dir}/bin:${PATH}" assert_rebuilt linux "${before}" 'the compression tool identity'
run_prepare linux
before="$(digest linux)"
printf '\n# builder input change\n' >>"${script}"
assert_rebuilt linux "${before}" 'the builder script'

# A dependency compilation failure must never allow reuse of the old helper.
before="$(digest linux)"
printf '\ninvalid Go source\n' >>"${repo}/internal/transitive/message.go"
if bash "${script}" linux amd64 >"${test_dir}/failure.log" 2>&1; then
  echo 'failed dependency compilation incorrectly reused the helper' >&2
  exit 1
fi
[[ "$(digest linux)" == "${before}" ]]
printf 'package transitive\nconst Message = "repaired"\n' >"${repo}/internal/transitive/message.go"
run_prepare linux

# A future import of the generated asset must fail explicitly rather than
# turning the previous helper into an input to the next helper.
cat >"${repo}/internal/shim/embed/embed.go" <<'EOF'
package embed
type Asset struct { SourceDigest, SHA256 string; Zstd []byte }
func register(Asset) {}
EOF
cat >"${repo}/cmd/shim/main.go" <<'EOF'
package main
import (
  "example.test/shim-cache/internal/app/install"
  _ "example.test/shim-cache/internal/shim/embed"
)
func main() { println(install.Message()) }
EOF
if bash "${script}" linux amd64 >"${test_dir}/cycle.log" 2>&1; then
  echo 'generated self-embedding dependency was incorrectly accepted' >&2
  exit 1
fi
grep -Fq 'must not depend on its generated' "${test_dir}/cycle.log"

echo 'shim embed cache selftest: passed'
