#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

check_roots=()
for dir in cmd internal testkit; do
  if [[ -d "${dir}" ]]; then
    check_roots+=("${dir}")
  fi
done

if [[ ${#check_roots[@]} -eq 0 ]]; then
  exit 0
fi

violations_file="$(mktemp)"
trap 'rm -f "${violations_file}"' EXIT

while IFS= read -r -d '' file; do
  [[ -n "${file}" ]] || continue

  line_count="$(wc -l < "${file}")"
  file_name="$(basename "${file}")"
  file_kind="business"
  limit=1000
  if [[ "${file_name}" == *_test.go ]]; then
    file_kind="test"
    limit=2000
  fi

  if (( line_count > limit )); then
    printf '%s\t%s\t%s\t%s\n' "${line_count}" "${limit}" "${file_kind}" "${file}" >> "${violations_file}"
  fi
done < <(find "${check_roots[@]}" -type f -name '*.go' -print0)

if [[ ! -s "${violations_file}" ]]; then
  exit 0
fi

echo "Go file length limits exceeded. Split the files locally before committing." >&2
echo "Limits: business files <= 1000 lines, test files <= 2000 lines." >&2
echo >&2

while IFS=$'\t' read -r line_count limit file_kind file; do
  printf '  - %s: %s lines (limit %s, %s file)\n' "${file}" "${line_count}" "${limit}" "${file_kind}" >&2
done < <(sort -t $'\t' -k1,1nr -k4,4 "${violations_file}")

exit 1
