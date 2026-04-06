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
- `./install.sh`
  - repo-local lifecycle helper for `bootstrap/start/stop/restart/refresh/status/logs`

These repo helpers are not part of the released product package.

## Before you test in Feishu

- make sure the app has the bot message/event permissions from `deploy/feishu/README.md`
- if you want local `.md` links to become Feishu preview links, also grant `drive:drive`

Then in Feishu:

- send `/list`
- reply with the instance number to attach
- use `/threads` to switch thread if needed
- remote execution defaults to full access; if you need confirmation mode temporarily, send `/access confirm`
