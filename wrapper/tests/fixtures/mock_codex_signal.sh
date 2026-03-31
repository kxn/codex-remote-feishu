#!/bin/bash
# Mock codex binary that logs received signals to a file.
# Requires MOCK_SIGNAL_LOG env var to be set.

LOG_FILE="${MOCK_SIGNAL_LOG:-/tmp/mock_codex_signals.log}"

trap 'echo "SIGINT" >> "$LOG_FILE"; exit 130' INT
trap 'echo "SIGTERM" >> "$LOG_FILE"; exit 143' TERM

# Write a ready marker so the test knows we're running
echo "READY"

# Wait indefinitely for signals
while true; do
    sleep 0.1
done
