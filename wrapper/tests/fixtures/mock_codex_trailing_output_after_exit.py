#!/usr/bin/env python3
import signal
import subprocess
import sys


def main() -> int:
    stdout_chunks = int(sys.argv[1]) if len(sys.argv) > 1 else 4
    stderr_chunks = int(sys.argv[2]) if len(sys.argv) > 2 else 4
    chunk_bytes = int(sys.argv[3]) if len(sys.argv) > 3 else 4096
    delay_secs = float(sys.argv[4]) if len(sys.argv) > 4 else 1.0

    for _ in sys.stdin:
        pass

    writer_code = r"""
import signal
import sys
import time

signal.signal(signal.SIGHUP, signal.SIG_IGN)

stdout_chunks = int(sys.argv[1])
stderr_chunks = int(sys.argv[2])
chunk_bytes = int(sys.argv[3])
delay_secs = float(sys.argv[4])

for index in range(max(stdout_chunks, stderr_chunks)):
    if index < stdout_chunks:
        sys.stdout.write("o" * chunk_bytes)
        sys.stdout.flush()
    if index < stderr_chunks:
        sys.stderr.write("e" * chunk_bytes)
        sys.stderr.flush()
    time.sleep(delay_secs)

sys.stdout.write("\n")
sys.stdout.flush()
"""

    subprocess.Popen(
        [
            sys.executable,
            "-c",
            writer_code,
            str(stdout_chunks),
            str(stderr_chunks),
            str(chunk_bytes),
            str(delay_secs),
        ],
        stdin=subprocess.DEVNULL,
        stdout=sys.stdout,
        stderr=sys.stderr,
        start_new_session=True,
    )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
