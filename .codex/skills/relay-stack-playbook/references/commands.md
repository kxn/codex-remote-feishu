# Commands

Use these commands for real-stack debugging in this repository.

## Proxy-safe test command

```bash
env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy go test ./...
```

## One-shot diagnostics

First verify the intended service and its install state as described in the playbook. Pass its actual base directory and ports; the helper defaults do not identify the target:

```bash
./scripts/relay/collect-diagnostics.sh --base-dir <verified-base-dir> --relay-port <verified-relay-port> --admin-port <verified-admin-port>
```

## Proxy-safe relay status query

Use the verified admin port and clear proxy variables when localhost requests may be intercepted (replace the placeholder with the verified numeric port):

```bash
env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy curl --noproxy '*' 'http://127.0.0.1:<verified-admin-port>/v1/status'
```

## Process and port checks

```bash
ps -ef | rg 'codex-remote|relayd|relay-wrapper' | rg -v rg
ss -ltnp
tail -n 200 '<verified-log-path>'
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
