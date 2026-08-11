#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

failures=()
tmp_files=()

cleanup() {
  if [[ ${#tmp_files[@]} -gt 0 ]]; then
    rm -f "${tmp_files[@]}"
  fi
}
trap cleanup EXIT

new_tmp_file() {
  local path
  path="$(mktemp "$1")"
  tmp_files+=("${path}")
  printf '%s' "${path}"
}

stage_blob() {
  local index_file="$1"
  local path="$2"
  local content="$3"
  local object_id
  object_id="$(printf '%s' "${content}" | git hash-object -w --stdin)"
  GIT_INDEX_FILE="${index_file}" git update-index --add --cacheinfo 100644 "${object_id}" "${path}"
}

with_temp_index() {
  local index_file
  index_file="$(new_tmp_file /tmp/docs-conventions-index.XXXXXX)"
  GIT_INDEX_FILE="${index_file}" git read-tree HEAD
  "$@" "${index_file}"
}

run_docs_conventions() {
  local index_file="$1"
  local output_file="$2"
  GIT_INDEX_FILE="${index_file}" bash scripts/check/docs-conventions.sh >"${output_file}" 2>&1
}

check_obsoleted_requires_superseded_by() {
  local index_file="$1"
  local output_file
  output_file="$(new_tmp_file /tmp/docs-conventions-output.XXXXXX)"
  stage_blob "${index_file}" "docs/obsoleted/docs-conventions-selftest.md" $'# Self Test\n\n> Type: `obsoleted`\n> Updated: `2026-08-11`\n> Summary: selftest fixture.\n'
  stage_blob "${index_file}" "docs/README.md" $'# Documentation Index\n\n> Type: `general`\n> Updated: `2026-08-11`\n> Summary: selftest fixture index update.\n'
  if run_docs_conventions "${index_file}" "${output_file}"; then
    failures+=("obsoleted doc without Superseded By unexpectedly passed")
    return
  fi
  if ! rg -q 'missing metadata line > Superseded By:' "${output_file}"; then
    failures+=("obsoleted failure did not mention missing Superseded By")
  fi
}

check_superpowers_docs_are_ignored() {
  local index_file="$1"
  local output_file
  output_file="$(new_tmp_file /tmp/docs-conventions-output.XXXXXX)"
  stage_blob "${index_file}" "docs/superpowers/plans/docs-conventions-selftest.md" $'Spec work item without lifecycle metadata.\n'
  if ! run_docs_conventions "${index_file}" "${output_file}"; then
    failures+=("docs/superpowers staged doc was not ignored")
  fi
}

check_lifecycle_add_still_requires_index() {
  local index_file="$1"
  local output_file
  output_file="$(new_tmp_file /tmp/docs-conventions-output.XXXXXX)"
  stage_blob "${index_file}" "docs/general/docs-conventions-selftest.md" $'# Self Test\n\n> Type: `general`\n> Updated: `2026-08-11`\n> Summary: selftest fixture.\n'
  if run_docs_conventions "${index_file}" "${output_file}"; then
    failures+=("lifecycle doc add without docs/README.md unexpectedly passed")
    return
  fi
  if ! rg -q 'docs add/delete/rename detected without staging docs/README.md' "${output_file}"; then
    failures+=("lifecycle add failure did not mention docs/README.md")
  fi
}

with_temp_index check_obsoleted_requires_superseded_by
with_temp_index check_superpowers_docs_are_ignored
with_temp_index check_lifecycle_add_still_requires_index

if [[ ${#failures[@]} -gt 0 ]]; then
  printf '%s\n' "${failures[@]}" >&2
  exit 1
fi

echo "docs-conventions selftest: passed"
