# AGENTS

## Proxy Environment

This repository is often developed on hosts where `http_proxy` / `https_proxy` are set globally.
Those variables frequently interfere with local testing, especially for:

- `curl http://127.0.0.1:...`
- local health checks
- websocket/http calls to local relay services
- integration tests that expect direct localhost access

Before running local tests or local debugging commands, clear proxy-related environment variables in the shell used for the test:

```bash
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy
```

Recommended rule:

- Default for local testing/debugging: proxy env must be unset.
- Default for localhost requests: proxy env must be unset.

## Wrapper Exception

There is one important exception:

- `relay-wrapper` itself should run without inheriting proxy env for its own local relay communication.
- But when `relay-wrapper` launches the real `codex` binary (`codex.real`), it must restore the captured proxy env for the child process.

Reason:

- local wrapper <-> relayd / localhost traffic is easily broken by proxy interception
- upstream `codex.real` <-> ChatGPT/OpenAI traffic is more stable when it uses the configured proxy

So the intended behavior is:

1. wrapper process starts and clears proxy env for itself
2. wrapper communicates with local relay services without proxy
3. wrapper spawns `codex.real` with the previously captured proxy env restored

Any future changes to startup, testing scripts, or process launching must preserve this rule.

## Stateful Debugging Rule

For bugs that involve multiple layers or state machines (for example VS Code <-> wrapper <-> relayd <-> Feishu):

- Do not patch the first plausible cause and stop.
- First collect runtime evidence from the full path: current server state, relevant logs, and the actual event/control flow.
- For protocol/render regressions, capture one real upstream payload and one actual downstream payload before changing code; do not reason only from mocks or remembered protocol shapes.
- Distinguish user-visible conversation traffic from editor/internal helper traffic before reusing templates or forwarding events. Internal helper fields such as structured-output schemas or ephemeral thread settings must not be treated as reusable chat defaults.
- Translate the user-reported reproduction into tests before or together with the fix.
- If multiple layers participate in the bug, fix the whole chain in one pass instead of doing isolated partial tweaks.
- Do not consider the issue fixed just because unit tests pass; verify that the observed runtime state actually changes in the expected way.

This rule exists because partial fixes on stateful flows often leave the visible behavior unchanged and waste debugging cycles.

## Config Preservation Rule

For installers, bootstrap commands, and config migration code:

- Never clear an existing credential, token, secret, or app key just because the current invocation omitted that flag or env var.
- Empty input means "preserve existing value" unless the product explicitly defines a destructive reset flow.
- Add a regression test for any config writer that touches persisted auth or integration settings.

## Service Lifecycle Rule

For local service control during debugging:

- Do not run mutating lifecycle commands for the same service in parallel. In particular, never overlap `stop`, `start`, `restart`, or `bootstrap` for one daemon.
- When validating a daemon restart, verify the post-start runtime state directly with `ps`, bound ports, and a real health/status call instead of trusting the shell script's success message.

## Protocol Correlation Rule

For app-server helper or internal traffic:

- Never suppress or classify helper turns by thread-local heuristics such as "same thread" or "next turn on this thread".
- Correlate helper thread/turn lifecycle only through protocol-level identifiers returned by the server, such as request `id -> result.thread.id` or `id -> result.turn.id`.
- If the real protocol provides an exact correlation handle, use it. Do not replace it with timing-based or adjacency-based guesses in production logic or mocks.

## Layer Ownership Rule

For wrapper/server protocol work:

- The wrapper is responsible for accurate translation and explicit annotation, not for product-side visibility policy.
- If a native lifecycle event is real app-server runtime traffic, prefer emitting it with canonical metadata such as `trafficClass` / `initiator` instead of silently swallowing it.
- Product decisions such as "pause queue", "render to Feishu", "hide helper traffic", or "update selected thread" belong in the server/orchestrator layer and must be tested there.
