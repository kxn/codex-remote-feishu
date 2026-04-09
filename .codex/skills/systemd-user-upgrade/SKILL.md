---
name: systemd-user-upgrade
description: "Upgrade this repository's `codex-remote` `systemd --user` deployment from a freshly built binary or release artifact. Use when the user wants to roll the daily-development daemon forward, keep the installed runtime separate from workspace build artifacts, verify real service health after restart, and automatically roll back if the new version fails."
---

# systemd-user-upgrade

Use this skill when updating the host's long-running `codex-remote.service` from a local build or release artifact.

## Guardrails

- Treat the current `systemd --user` deployment as the source of truth. Read `install-state.json` first and refuse to guess install paths.
- Keep workspace artifacts out of the live runtime. Always stage the provided artifact into the backup directory, then let `codex-remote install` copy it into the existing installed binary directory.
- Verify the current deployment is healthy before changing it unless the user explicitly wants an emergency recovery run.
- Clear proxy-related env vars for localhost checks.
- Do not trust `systemctl start` alone. Verify:
  - `systemctl --user is-active`
  - relay/admin ports are listening
  - `GET /v1/status` succeeds locally
- On any failed upgrade check, roll back the binary, install state, config, and unit file, then restart and verify the old version.

## Default Workflow

1. Build the new artifact outside the workspace if needed.
2. Run:

```bash
python3 .codex/skills/systemd-user-upgrade/scripts/upgrade_systemd_user_service.py /abs/path/to/codex-remote
```

3. Read the printed summary:
  - previous version
  - target version
  - live installed binary path
  - backup directory
4. If the script exits non-zero, inspect the rollback result and recent journal lines printed by the script before making more changes.

## Common Options

- Use `--state-path` only for another default-layout install root. The script intentionally rejects custom state file layouts.
- Use `--backup-root` to store backup bundles somewhere else.
- Use `--allow-unhealthy-current` only for recovery work when the current deployment is already broken and there is no healthy rollback baseline.
- Use `--timeout-seconds` if the host normally needs longer to bring the daemon and its headless workers back up.

## Resource

- [`scripts/upgrade_systemd_user_service.py`](./scripts/upgrade_systemd_user_service.py): deterministic upgrade runner with backup, health checks, rollback, and journal tail on failure.
