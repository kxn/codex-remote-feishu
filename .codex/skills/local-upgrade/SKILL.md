---
name: local-upgrade
description: "Use when the user asks for this repository's local upgrade flow: 本地升级, 拉最新代码后升级本地 daemon, upgrade-local.sh, or triggering the built-in local upgrade transaction from a repo build. Prefer the repo helper script and fixed local-upgrade artifact path; do not use the removed install -upgrade-source-binary flag."
---

# local-upgrade

Use this skill when the task is to refresh the locally installed `codex-remote` daemon from the current repository checkout.

## Default path

Prefer this command from the repo root:

```bash
./upgrade-local.sh
```

That script does all of the following:

1. `git pull --ff-only`
2. rebuild `./bin/codex-remote`
3. copy the new binary to the fixed local artifact path
4. run `./bin/codex-remote local-upgrade`

## Natural-Language Boundary

- Natural-language repo requests such as `本地升级`, `debug 一下`, or `看下当前实例状态` are **repository tasks**, not daemon slash-command requests.
- For those repo tasks:
  - use `./upgrade-local.sh` for upgrade
  - use `bash scripts/install/repo-install-target.sh --format shell` or `bash scripts/install/repo-target-request.sh ...` for bound-instance status/debug requests
  - do **not** send `/upgrade ...` or `/debug ...` back into whichever daemon is currently hosting the Codex conversation
- Explicit slash commands such as `/upgrade`, `/upgrade local`, `/upgrade latest`, and `/debug` remain direct daemon actions on the daemon that received that slash command.

## Variants

- Different install base dir:

```bash
./upgrade-local.sh --base-dir /path/to/base
```

- Explicit slot label:

```bash
./upgrade-local.sh --slot local-test
```

- Dirty worktree:
  - default behavior is to stop before `git pull`
  - only use `--allow-dirty` when the user explicitly wants to keep going despite local changes

## Notes

- The built-in CLI entry is `codex-remote local-upgrade`.
- The fixed artifact path is `~/.local/share/codex-remote/local-upgrade/codex-remote` for the default base dir.
- If the script says `install-state.json` is missing, bootstrap the local install first with `./setup.sh` or point `--base-dir` at the installed environment.
- For repo-bound debug/status HTTP calls, prefer:

```bash
bash scripts/install/repo-target-request.sh admin /v1/status | jq .
```

```bash
bash scripts/install/repo-target-request.sh admin /api/admin/bootstrap-state | jq .
```

- For explanation-only requests, `./upgrade-local.sh --help` is usually enough.
