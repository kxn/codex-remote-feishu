# Relay Error Reporting Protocol

> Type: `general`
> Updated: `2026-04-09`
> Summary: 说明 relay 链路错误如何回传到 Feishu，并区分 gateway apply 失败与 final preview rewrite 降级。

## Goal

When any layer in the relay stack fails, Feishu should receive a visible debug card instead of silent timeout/log-only failure.

The card should answer three questions immediately:

- which layer failed
- where it failed
- what raw debug information is available right now

## Unified Payload

All layers now share one structured payload: `agentproto.ErrorInfo`.

Fields:

- `code`: stable error code such as `translate_command_failed`
- `layer`: failing layer, for example `daemon`, `wrapper`, `relayws_server`
- `stage`: exact failing step, for example `gateway_apply`, `translate_command`
- `operation`: higher-level operation, for example `prompt.send`, `codex.stdout`
- `message`: human-readable summary for Feishu
- `details`: raw debug string for immediate triage
- `surfaceSessionId`: optional direct Feishu target
- `commandId` / `threadId` / `turnId` / `requestId`: protocol correlation handles
- `retryable`: whether retry is likely meaningful

## Wire-Level Uses

The same `ErrorInfo` schema is now reused in three protocol paths:

1. `agentproto.Event{kind: "system.error"}`
   Used for asynchronous runtime failures that are not tied to a single command ack.

2. `agentproto.CommandAck.problem`
   Used when wrapper rejects an inbound relay command, so the pending Feishu input can fail with a structured reason instead of a flat string.

3. `agentproto.ErrorEnvelope.problem`
   Used for websocket-level relay transport errors such as malformed envelopes.

## Feishu Delivery Rules

### Normal async/system errors

`system.error` is routed by this priority:

1. explicit `surfaceSessionId`
2. `commandId` correlation against pending/active remote bindings
3. `threadId` / `turnId` correlation against active remote turn ownership
4. fallback to all attached Feishu surfaces for the instance

The orchestrator converts the payload into a notice card with:

- title: `链路错误 · <layer>.<stage>`
- body: layer, stage, operation, correlation ids, summary, and fenced raw details

### Command rejection errors

If wrapper rejects a relay command, daemon uses `CommandAck.problem` to:

- mark the pending Feishu queue item as failed
- stop typing reaction
- send the same structured debug notice to Feishu

### Gateway apply failures

If daemon fails while sending a card/message to Feishu, it cannot report that failure immediately through the same failed request.

This stage is specifically about the outbound Feishu send itself. For example, final markdown preview rewrite may fail earlier and degrade locally, but that does not count as `daemon.gateway_apply` unless the later Feishu send also fails.

Best-effort fallback:

- daemon builds the same structured debug notice locally
- it queues that notice per `surfaceSessionId`
- on the next successful outbound event for that surface, daemon flushes the queued debug notice first

This does not guarantee visibility during a total Feishu outage, but it removes the common “one send failed and nothing was visible later” problem.

## Current Instrumented Failure Points

- `relayws_client.decode_envelope`
- `relayws_server.decode_envelope`
- `relayws_server.hello`
- `wrapper.translate_command`
- `wrapper.observe_parent_stdin`
- `wrapper.observe_codex_stdout`
- `wrapper.forward_client_events`
- `wrapper.forward_server_events`
- `wrapper.write_codex_stdin`
- `wrapper.write_parent_stdout`
- `daemon.dispatch_prepare`
- `daemon.dispatch_command`
- `daemon.relay_send_command`
- `daemon.send_threads_refresh`
- `daemon.gateway_apply`

## Non-Goals

- This does not replace raw logs.
- This does not guarantee Feishu visibility during a complete gateway outage.
- This does not yet persist historical error cards into `/status`.

## Validation

Automated tests cover:

- relay websocket error envelope callback
- structured rejected ack without dropping the connection
- `system.error` projection to Feishu notice
- gateway failure queue and later flush
