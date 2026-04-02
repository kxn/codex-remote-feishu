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

Additional surface guidance:
- `bot-service`: up to **3** concurrent validators. These tests are mock-heavy, CPU-light, and do not bind fixed ports.
- `relay-bot-integration`: up to **2** concurrent validators. Each run may build shared artifacts and spawn wrapper/server processes plus temp workspaces, so keep this surface below the global ceiling.

## Setup Notes

- Start the live relay server with `cd /data/dl/fschannel/server && RELAY_API_PORT=9501 RELAY_PORT=9500 node dist/index.js`.
- Confirm the API is healthy with `curl --noproxy '*' -sf http://localhost:9501/sessions`.
- For the `feishu-bot` milestone, build the shared package, server package, and wrapper binary before running cross-package integration validators: `cd /data/dl/fschannel/shared && npm run build`, `cd /data/dl/fschannel/server && npm run build`, and `cd /data/dl/fschannel/wrapper && cargo build`.
- The `bot/src/integration-core-flows.test.ts` suite starts its own ephemeral relay server instances; keep the shared live relay on `9500/9501` for targeted curl/manual edge checks only.
- No seed data is required for the `core-infra` milestone; all validation uses ephemeral mock codex processes, temporary files, and in-memory server state.
- No additional seed data is required for the `feishu-bot` milestone either; bot validation uses mocked Feishu SDK interactions, temp workspaces, and in-memory relay state.

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

## Flow Validator Guidance: bot-service

- Surface: `bot/src/commands.test.ts`, `bot/src/formatter.test.ts`, `bot/src/relay.test.ts`, `bot/src/feishu.test.ts`, and `bot/src/bot-service.test.ts`.
- Safe parallelism: up to 3 validators at once on this surface. The tests use fake timers, mocked relay responses, and mocked Feishu SDK calls, so they do not need shared ports or external services.
- Isolation boundaries:
  - Stay within `/data/dl/fschannel/bot`, the shared workspace packages, and temporary files under `/tmp` if you need scratch data.
  - Do not contact the real Feishu API or a real relay unless the assigned assertion explicitly calls for a live manual check.
  - In any inline scripts, use unique user/chat/session identifiers per validator to avoid confusing evidence.
- Evidence to capture: command output, specific passing test names, and observed attach summaries, forwarded messages, approval prompts, menu handling, and notification text that map back to the assigned assertions.

## Flow Validator Guidance: relay-bot-integration

- Surface: `bot/src/integration-core-flows.test.ts` plus targeted `node --input-type=module` or `curl` checks that exercise the real relay HTTP/WebSocket surfaces, the compiled wrapper binary, and the bot service with mocked Feishu delivery.
- Safe parallelism: run at most 2 validators at once on this surface. Each validator may build shared artifacts and spawn relay/wrapper child processes in temporary workspaces.
- Isolation boundaries:
  - Prefer the integration suite's ephemeral ports. If you use the shared relay on `9500/9501`, reserve unique session names, user IDs, and temp workspaces for your validator only.
  - Use temp directories under `/tmp` for mock codex scripts and logs, and clean up any child processes you start.
  - Never call the real Feishu network; keep messenger/SDK behavior mocked or recorded locally.
  - `bot/src/integration-core-flows.test.ts` still exercises shutdown via `server.close()` rather than a real OS signal, so `VAL-CROSS-020` should continue to use a dedicated isolated Node harness on reserved ports whenever signal-handling regressions are suspected.
  - The dedicated SIGTERM harness should launch `server/dist/index.js`, attach through `BotService`, send `SIGTERM` to the server PID, and expect the attached-user message `Relay server is unavailable; detached from [<session>]. Local proxy will continue until the relay reconnects.` while the wrapper keeps local stdio proxying. A robustness rerun on 2026-04-02 passed with server exit code `0`, wrapper local proxy preserved, and no console/unhandled errors.
- Evidence to capture: messenger outputs, relay session snapshots, logged wrapper inputs, reconnect/offline state observations, and any approval or attachment transitions needed to prove the assigned assertions.
