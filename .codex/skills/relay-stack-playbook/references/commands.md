# Commands

Use these commands for real-stack debugging in this repository.

## Proxy-safe test command

```bash
env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy go test ./...
```

## One-shot diagnostics

Prefer this first when you need the fixed evidence bundle:

```bash
./scripts/relay/collect-diagnostics.sh
```

## Proxy-safe relay status query

Prefer this over `curl` when localhost requests may be intercepted:

```bash
env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy bash -lc 'exec 3<>/dev/tcp/127.0.0.1/9501 && printf "GET /v1/status HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n" >&3 && cat <&3'
```

## Process and port checks

```bash
ps -ef | rg 'relayd|relay-wrapper' | rg -v rg
ss -ltnp | rg '9500|9501'
tail -n 200 /home/dl/.local/share/codex-relay/logs/relayd.log
```

## Symptom shortcuts

If VS Code shows a result but Feishu does not:

- inspect `/v1/status`
- check whether any `surface` exists
- check `gateway apply failed` in `relayd.log`

If `/list` or `/attach` works but later text has no effect:

- inspect surface attachment and route state in `/v1/status`
- verify menu events and text events use the same `surfaceSessionID`

If a restart changes behavior unexpectedly:

- verify the actual `relayd` process and ports after restart
- treat previous Feishu attach state as invalid until re-established
