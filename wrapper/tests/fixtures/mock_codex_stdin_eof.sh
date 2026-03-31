#!/bin/bash
# Mock codex binary that detects stdin EOF and writes a marker to a file.
# Requires MOCK_EOF_LOG env var to be set.

LOG_FILE="${MOCK_EOF_LOG:-/tmp/mock_codex_eof.log}"

# Read stdin until EOF
while IFS= read -r line; do
    printf '%s\n' "$line"
done

# EOF detected - write marker
echo "EOF_DETECTED" > "$LOG_FILE"
exit 0
