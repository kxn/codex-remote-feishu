---
name: relay-stack-playbook
description: "Use when working on this repository's relay stack: Codex app-server protocol translation, relayd, relay-wrapper, Feishu bot integration, VS Code remote integration, or real-stack debugging around /list, /attach, /use, thread routing, helper/internal traffic, and missing Feishu replies. Summarizes the correct execution order, validation steps, and common failure modes specific to this project."
---

# relay-stack-playbook

Use this skill for changes or debugging involving:

- `relayd`
- `relay-wrapper`
- Feishu inbound/outbound behavior
- VS Code remote integration
- Codex app-server protocol translation
- `/list`, `/attach`, `/use`, `/stop`, thread routing, queue state, missing replies

## Core rules

- Start from runtime evidence, not the first plausible fix.
- Split the problem by layer before editing code:
  - `wrapper`: native protocol translation and explicit annotation
  - `server/orchestrator`: product state, queue, thread routing, render decisions
  - `feishu gateway`: inbound action parsing and outbound delivery
- The wrapper must annotate helper/internal traffic; it must not silently swallow real runtime lifecycle events.
- Helper lifecycle must be correlated by protocol ids such as `request id -> result.thread.id` and `request id -> result.turn.id`. Do not use "same thread" or timing heuristics.
- Product visibility decisions belong in the server layer, not in the wrapper.
- Do not trust mocks unless they match real frames captured from logs.

## First checks

Before changing code, gather these facts in order:

0. Prefer the repo helper script first.
   - Run `./scripts/relay/collect-diagnostics.sh`.
   - It captures the fixed low-level evidence in one pass:
     - proxy env
     - service status
     - process and port checks
     - `/api/admin/bootstrap-state`
     - `/v1/status`
     - recent relayd logs

1. Check relay runtime state.
   - Read `relayd` status.
   - Verify actual processes and listening ports.
2. Query `/v1/status` from localhost without proxy interference.
   - Prefer the raw local socket command in `references/commands.md`.
   - Confirm:
     - instance is online
     - `ObservedFocusedThreadID`, `ActiveThreadID`, `ActiveTurnID`
     - surface count
     - attached instance id
     - selected thread id
     - dispatch mode
     - active and queued queue items
3. Read recent `relayd` logs.
   - Look for:
     - `relay instance connected`
     - `relay instance disconnected`
     - `gateway apply failed`
     - port bind failures
4. If VS Code shows the result but Feishu does not:
   - wrapper -> relay path is at least partially working
   - inspect relay surface state and gateway failures first
5. If `/list` or `/attach` seems to work but later text goes nowhere:
   - verify inbound menu events and text messages resolve to the same `surfaceSessionID`
   - check whether `surfaces=0` or whether the current chat is attached to a different surface

## Change strategy

- For protocol changes, update docs and implementation together.
- Add or update tests at the same time as the fix.
- Prefer fixing the whole failing chain in one pass:
  - translator unit tests
  - orchestrator state tests
  - wrapper integration tests
  - harness end-to-end tests
- Do not stop after a local unit test passes if the user-reported runtime symptom is still unexplained.

## Common mistakes in this repo

- Reusing helper traffic templates as normal chat defaults.
- Hiding helper lifecycle in the wrapper instead of marking it and filtering later.
- Debugging only from mocks even when the real protocol already disagrees.
- Treating a proxy-intercepted `curl` response as a real localhost response.
- Restarting services during manual reproduction and then reasoning from stale Feishu/VS Code state.
- Changing only one layer in a multi-layer bug and assuming the visible behavior will follow.
- Trusting "started" output from service control without checking the actual process and ports.

## Validation

- Clear proxy variables before local tests and localhost checks.
- Prefer `./scripts/relay/collect-diagnostics.sh` for fixed evidence collection, then use the lower-level commands in `references/commands.md` when you need a narrower repro.
- After protocol or state-machine edits, run:
  - `go test ./...`
- When the bug is user-visible, verify the exact symptom path:
  - Feishu input
  - relay state change
  - wrapper/native activity
  - Feishu output or gateway failure

## References

- For commands and symptom-driven checks, read `references/commands.md`.
