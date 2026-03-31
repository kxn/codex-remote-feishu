#!/bin/bash
# Mock codex binary that exits with a specific code.
# Usage: mock_codex_exit.sh <exit_code>
# Or set MOCK_EXIT_CODE env var.

EXIT_CODE="${1:-${MOCK_EXIT_CODE:-0}}"
exit "$EXIT_CODE"
