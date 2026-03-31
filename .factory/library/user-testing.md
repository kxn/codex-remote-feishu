# User Testing

Testing surface, required testing skills/tools, and resource cost classification.

---

## Validation Surface

This is a CLI/service project with no web UI. All validation is done through:

1. **Custom test scripts**: End-to-end flows using mocked components (mock codex process, mock Feishu SDK)
2. **curl**: REST API endpoint testing for the relay server
3. **cargo test**: Rust unit/integration tests for the wrapper
4. **vitest**: TypeScript unit/integration tests for server, bot, shared

### Testing Tools
- `curl` for HTTP API assertions
- Custom Node.js test scripts for end-to-end flows
- Mock codex binary (simple Rust or shell script that reads/writes JSONL)
- Mock Feishu SDK (intercept `@larksuiteoapi/node-sdk` calls)

### Surfaces NOT Tested
- Real Feishu integration (deferred until credentials provided)
- Real Codex CLI integration (mocked in all tests)

## Validation Concurrency

Machine: 94GB RAM, 32 cores, ~9GB used at baseline.

All validation is lightweight (no browsers, no heavy processes):
- Each test script: ~50MB RAM
- Server process: ~100MB RAM
- Wrapper process: ~5MB RAM

Max concurrent validators: **5** (well within resource budget)

## Setup Notes

- Start the live relay server with `cd /data/dl/fschannel/server && RELAY_API_PORT=9501 RELAY_PORT=9500 node dist/index.js`.
- Confirm the API is healthy with `curl --noproxy '*' -sf http://localhost:9501/sessions`.
- No seed data is required for the `core-infra` milestone; all validation uses ephemeral mock codex processes, temporary files, and in-memory server state.

## Flow Validator Guidance: wrapper-cli

- Surface: the compiled Rust wrapper binary exercised through `cargo test` integration tests in `wrapper/tests/`.
- Safe parallelism: multiple wrapper validators may run at once because the tests use temp files, temp directories, and ephemeral localhost ports. Keep them within the repo checkout and `/tmp`.
- Isolation boundaries:
  - Use only fixture scripts under `wrapper/tests/fixtures/`.
  - Do not bind fixed ports outside ephemeral OS-assigned ports.
  - Do not talk to the live relay service on `9500/9501` unless a specific assertion requires it; the wrapper tests already spin up isolated mock relays.
- Evidence to capture: exact test command, passing test names, and any observed stdout/stderr/log-file behavior that maps to the assigned assertions.

## Flow Validator Guidance: relay-api

- Surface: the real relay server interfaces exercised through `curl` and the integration-style Vitest suite in `server/src/relay-server.test.ts`.
- Safe parallelism: relay API validators may run concurrently with wrapper validators, but relay API validators that bind an actual server process should use random ports or the shared live service on `9500/9501` without mutating global state outside their assigned session IDs.
- Isolation boundaries:
  - When using the shared live service, create unique session IDs/user IDs per validator.
  - Use `curl --noproxy '*'` for localhost requests so proxy env vars do not interfere.
  - For manual websocket checks, run `node --input-type=module` from `/data/dl/fschannel/server` so `import { WebSocket } from "ws"` resolves against the server workspace dependency.
  - `POST /sessions/:id/input` expects a prompt payload such as `{"type":"prompt","content":"hello remote"}` (or another schema-supported relay input object); a bare `{"content":"..."}` body returns HTTP 400.
  - Prefer the Vitest suite's ephemeral ports for reconnect and malformed-input scenarios.
- Evidence to capture: HTTP status codes, REST payloads, WebSocket events, and history/attach state observations tied to the assigned assertions.
