# Quick Start

## Option 1: One-line install on macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

This command will:

1. Detect your platform
2. Download the GitHub-built release archive
3. Extract it under your local release cache
4. Install `codex-remote` to a stable local path
5. Start the local daemon and print the WebSetup URL

To pin a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --version v1.0.0
```

To install the latest beta track instead of the latest production build:

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash -s -- --track beta
```

## Option 2: Download a release archive

1. Download the archive matching your platform from GitHub Releases
2. Extract it
3. Run:

macOS / Linux:

```bash
./codex-remote install -bootstrap-only -start-daemon
```

Windows PowerShell:

```powershell
.\codex-remote.exe install -bootstrap-only -start-daemon
```

## Finish setup in the Web UI

After the daemon starts, open the printed `/setup` URL.

In WebSetup:

1. Add or verify your Feishu app credentials
2. Let the page detect your VS Code environment
3. Apply `editor_settings` or `managed_shim`
4. Reinstall shim after extension upgrades when the page asks for it

## Repo-only helpers

If you are working from a source checkout instead of a release archive:

- `./setup.sh` or `./setup.ps1`
  - builds a local binary
  - bootstraps the daemon
  - opens or prints the same WebSetup flow
- `./bin/codex-remote install -bootstrap-only -start-daemon`
  - reruns repo-local bootstrap directly from the built binary
- `./bin/codex-remote daemon`
  - runs the daemon in the foreground for targeted debugging

These repo helpers are not part of the released product package.

## Linux `systemd --user` service

If you want the Linux daemon to be managed as a long-running user service instead of a detached process:

```bash
codex-remote service install-user
codex-remote service enable
codex-remote service start
codex-remote service status
```

If you also want it to come back after reboot before opening a terminal session, enable lingering for your user:

```bash
loginctl enable-linger "$USER"
```

## Upgrade From A Local Build

If you already built a fresh local binary and want to roll the installed daemon forward through the built-in upgrade transaction:

```bash
./bin/codex-remote install -upgrade-source-binary ./bin/codex-remote -upgrade-slot local-$(git rev-parse --short HEAD)
```

This imports the local binary into `versionsRoot/<slot>`, runs the same upgrade helper transaction, and automatically rolls back if the new daemon does not recover to a healthy state.

## Before you test in Feishu

- make sure the app has the bot message/event permissions from `deploy/feishu/README.md`
- if you want local `.md` links to become Feishu preview links, also grant `drive:drive`

Then in Feishu:

- send `/help` or `menu` first if you want to see the current command set without guessing; `/help` stays text-first, while `/menu` now reorders its homepage by stage and uses compact button cards
- send `/list` if you want to explicitly attach one of the online VS Code instances
- send `/use` if you want to jump straight into a recent visible session; `/threads` is still accepted as a legacy alias; use `/useall` for the full list
- use the card buttons when they appear; if a card says it is stale or expired, resend the command instead of replying with a number
- final replies will show up under the source message that triggered them, which makes group chat context easier to follow
- if a text is still queued while another reply is running, add a `ThumbsUp` to that queued text to turn it into a follow-up for the current turn
- `/detach` drops the current attachment and also cancels a pending background recovery if one is in progress
- remote execution defaults to full access; if you need confirmation mode temporarily, send `/access confirm`; bare `/access` and bare `/reasoning` will both return parameter cards
