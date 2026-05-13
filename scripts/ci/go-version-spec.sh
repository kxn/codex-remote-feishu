#!/usr/bin/env bash
set -euo pipefail

mod_file="${1:-go.mod}"

if [[ ! -f "${mod_file}" ]]; then
  echo "go version spec source not found: ${mod_file}" >&2
  exit 1
fi

directive_value="$(
  awk '
    $1 == "toolchain" {
      print $2
      exit
    }
    $1 == "go" && go_version == "" {
      go_version = $2
    }
    END {
      if (go_version != "") {
        print go_version
      }
    }
  ' "${mod_file}"
)"

if [[ -z "${directive_value}" ]]; then
  echo "failed to resolve Go version from ${mod_file}" >&2
  exit 1
fi

version="${directive_value#go}"

if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?([-.][0-9A-Za-z.]+)?$ ]]; then
  echo "unsupported Go version directive: ${directive_value}" >&2
  exit 1
fi

if [[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf '~%s\n' "${version}"
  exit 0
fi

printf '%s\n' "${version}"
