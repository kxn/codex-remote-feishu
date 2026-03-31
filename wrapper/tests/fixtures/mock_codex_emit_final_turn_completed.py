import json
import sys


def main() -> int:
    payload = {
        "method": "turn/completed",
        "params": {
            "threadId": "thread-final",
            "turnId": "turn-final",
        },
    }
    sys.stdout.write(json.dumps(payload))
    sys.stdout.flush()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
