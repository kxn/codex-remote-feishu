---
name: local-upgrade
description: "Use when the user asks for this repository's local upgrade flow: 本地升级, 拉最新代码后升级本地 daemon, upgrade-local.sh, or triggering the built-in local upgrade transaction from a repo build. Prefer the repo helper script and fixed local-upgrade artifact path; do not use the removed install -upgrade-source-binary flag."
---

# local-upgrade

Use this skill to upgrade the locally installed `codex-remote` daemon from a repository checkout. The normal deployment has one daemon; do not infer a second service from a branch, checkout, historical label, or stale repository binding.

## Identify the target first

- An explicit user target takes precedence. For a session-hosted request, use the session's actual runtime identity when available and verify it against the running service and its install state.
- If there is no session environment or trustworthy worktree binding, inspect the running service (on Linux, `systemctl --user status codex-remote.service`), its process, service unit and command line. Find the corresponding `install-state.json` and compare its `instanceId`, `serviceUnitPath`, and `currentBinaryPath` with the running service. Resolve the config and base directory from the installation and service environment; those paths need not be fields in install-state. Confirm the actual admin port and logs. Do not rely on a leftover main-repository binding to select a target.
- Inspect `bash scripts/install/repo-install-target.sh` output before using the repository helper: it resolves a repository target and may select an old binding. `self-install-target.sh` and `self-target-request.sh` require a resolvable runtime environment; they are not evidence of a target when that environment is absent.
- If evidence is missing, contradictory, or identifies more than one plausible service, do not guess or operate on another instance; obtain the missing target information. Do not bootstrap merely because a guessed install-state path is missing.

## Upgrade

After verifying that its resolved target is the intended running installation, use:

```bash
./upgrade-local.sh
```

If the repository binding does not match, explicitly pass the verified `--instance <id>` and/or `--base-dir <dir>` as needed, and check the resolver output matches the intended state path before invoking the upgrade. The helper runs `git pull --ff-only`, builds the binary, stages the fixed local-upgrade artifact and invokes the built-in local upgrade transaction. Its default rejects tracked dirty changes; use `--allow-dirty` only when the user explicitly wants that.

For an explicitly identified session-hosting daemon whose self target is verifiable, `./upgrade-self.sh` builds a fresh binary and targets that daemon. Explicit daemon slash commands act on the daemon that receives them. Do not send a slash command as a substitute for a requested repository build.

Run service lifecycle and upgrade operations serially; do not overlap stop, start, restart or bootstrap for the same daemon. For local status or logs, use the verified installation's admin address and log path, clear proxy variables for localhost, and verify the actual process and port. The `scripts/install/*-target-request.sh` helpers are appropriate only when their resolved target has been verified.

For implementation details see `docs/general/local-self-upgrade-flow.md`; `./upgrade-local.sh --help` lists supported options. The default base directory's artifact is `~/.local/share/codex-remote/local-upgrade/codex-remote`.
