#!/bin/bash
# Mock codex binary that echoes stdin lines to stdout.
# Optionally writes a message to stderr if MOCK_STDERR_MSG is set.
# Optionally creates a sentinel file if MOCK_SENTINEL_FILE is set.

if [ -n "$MOCK_SENTINEL_FILE" ]; then
    touch "$MOCK_SENTINEL_FILE"
fi

if [ -n "$MOCK_STDERR_MSG" ]; then
    echo "$MOCK_STDERR_MSG" >&2
fi

if [ -n "$MOCK_ARGS_LOG" ]; then
    printf '%s\n' "$@" > "$MOCK_ARGS_LOG"
fi

if [ -n "$MOCK_ENV_CAPTURE_FILE" ] && [ -n "$MOCK_ENV_CAPTURE_KEYS" ]; then
    : > "$MOCK_ENV_CAPTURE_FILE"
    OLD_IFS="$IFS"
    IFS=','
    for key in $MOCK_ENV_CAPTURE_KEYS; do
        value=$(printenv "$key" 2>/dev/null || true)
        printf '%s=%s\n' "$key" "$value" >> "$MOCK_ENV_CAPTURE_FILE"
    done
    IFS="$OLD_IFS"
fi

# Echo stdin to stdout line by line
while IFS= read -r line; do
    printf '%s\n' "$line"
done

exit 0
