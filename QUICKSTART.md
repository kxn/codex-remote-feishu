# Quick Start

## Option 1: One-line install on macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.sh | bash
```

This command will:

1. Detect your platform
2. Download the latest release package
3. Extract it under your local release cache
4. Start the packaged interactive installer

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
./setup.sh
```

Windows PowerShell:

```powershell
.\setup.ps1
```

## After installation

Start the relay service on Linux with:

```bash
./install.sh start
```

Then in Feishu:

- send `/list`
- reply with the instance number to attach
- use `/threads` to switch thread if needed
