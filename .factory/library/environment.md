# Environment

Environment variables, external dependencies, and setup notes.

**What belongs here:** Required env vars, external API keys/services, dependency quirks, platform-specific notes.
**What does NOT belong here:** Service ports/commands (use `.factory/services.yaml`).

---

## Required Environment Variables

| Variable | Component | Description |
|----------|-----------|-------------|
| `RELAY_PORT` | Server | WebSocket port for wrapper connections (default: 9500) |
| `RELAY_API_PORT` | Server | HTTP REST API port (default: 9501) |
| `SESSION_GRACE_PERIOD` | Server | Seconds to keep offline sessions before eviction (default: 300) |
| `MESSAGE_BUFFER_SIZE` | Server | Max messages per session history buffer (default: 100) |
| `FEISHU_APP_ID` | Bot | Feishu app ID from developer console |
| `FEISHU_APP_SECRET` | Bot | Feishu app secret from developer console |
| `RELAY_API_URL` | Bot | Relay server REST API URL |
| `RELAY_SERVER_URL` | Wrapper | Relay server WebSocket URL |
| `CODEX_REAL_BINARY` | Wrapper | Path to real codex binary (default: `codex` in PATH) |

## External Dependencies

- **Feishu Open Platform**: Bot uses `@larksuiteoapi/node-sdk` WSClient (WebSocket long connection). No public HTTP endpoint needed.
- **OpenAI Codex CLI**: The real codex binary must be installed and accessible. Wrapper proxies its stdio.

## Platform Notes

- Wrapper targets Linux x86_64 (primary) and macOS (secondary)
- Signal forwarding (SIGINT, SIGTERM) is Unix-specific
- Rust 1.85+ required for wrapper compilation
- This environment exports `http_proxy` / `https_proxy`; use `curl --noproxy '*' http://localhost:...` for local relay API health checks or localhost requests may be routed through the proxy instead.
- This Debian image does not include `cargo clippy` or `cargo fmt` on `PATH` by default. To run `clippy` without root, download the `rust-clippy` `.deb`, extract it to a temp dir, and prepend the extracted `usr/bin` directory to `PATH` for the validator command.
- Node.js 22+ required for server and bot
