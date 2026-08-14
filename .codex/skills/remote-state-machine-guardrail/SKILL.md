---
name: remote-state-machine-guardrail
description: Audit and update this repository's canonical remote surface state machine after implementing changes to remote-surface state-machine logic carriers such as attach/detach/use/follow state predicates, routing decisions, headless lifecycle progression, queue/dispatch gating, request-capture gates, or command-availability logic. Use after implementation stabilizes and before committing. Reopen `docs/general/remote-surface-state-machine.md`, sync the current behavior, audit dead or half-dead user states, fix bug-grade issues found by that audit, and append unresolved product tradeoffs under `待讨论取舍`.
---

# Remote State Machine Guardrail

Treat [docs/general/remote-surface-state-machine.md](../../../docs/general/remote-surface-state-machine.md) as the canonical reference for current remote surface behavior.

Trigger this skill when the change touches remote-surface state-machine logic carriers, even if the intended product behavior was "not supposed to change".

Typical triggers:

- attach / detach / `/use` / `/follow` / `/new` state predicates or transition logic
- selected-thread / attached-instance / input-routing decisions
- queue / dispatch / pause-handoff gating logic
- headless launch / resume / cancel / timeout / recovery progression logic
- request capture / prompt gate / modal gate / staged-input enter-exit logic
- command-availability matrix logic

Do not trigger this skill for pure copy, styling, logging, tests, or refactors that leave those logic carriers unchanged.

Use this skill once per implementation pass, after the code and tests are mostly stable and before committing. Do not trigger it after every tiny edit.

## Fast Path For `state-light`

Use this path when the repo task is classified as `state-light`: it touches a remote-surface state-machine carrier, but does not change persistence format, protocol contracts, queue/recovery concurrency, permissions/security, or cross-component async lifecycle.

1. Read only the canonical document section(s) that describe the touched state, command, or transition. Locate sections with headings or exact command/state names before opening excerpts.
2. Read the touched code paths.
3. Sync only the affected current-behavior text in the canonical document.
4. Audit the changed path for dead or half-dead states.
5. Run focused tests for the touched path and `scripts/check/pre-commit.sh`.

Escalate to the full workflow below if the section read shows stale/conflicting state, the change crosses daemon/gateway/wrapper boundaries, the behavior depends on async recovery/queue timing, or the dead-state audit finds a bug-grade issue outside the edited path.

## Workflow

1. Read the canonical state machine document and the code paths touched by the change.
2. Update the document so it matches the current implementation:
   - route states
   - execution and dispatch states
   - modal or gate states
   - command matrix
   - async transitions and recovery paths
   - current bug-grade dead or half-dead states
3. Audit the changed behavior for user-dead-end transitions:
   - the UI looks attached or selected, but text/image/request input still cannot proceed
   - a blocked state has no visible escape hatch
   - stale cards or stale modal state can still intercept input
   - drafts or staged inputs can silently retarget to another thread
   - local-vs-remote arbitration can pause forever
   - detach or abandon can lock the surface forever
4. If the audit reveals a bug-grade issue, fix it before commit, add or update tests, then run this audit flow one more time.
5. If the remaining issue is a product tradeoff instead of a bug, append a short bullet list to `## 待讨论取舍` at the end of the canonical document:
   - decision needed
   - states and transitions affected
   - safest default if the choice remains unresolved

## Update Rules

1. Keep “current implemented behavior” separate from “recommended future changes”.
2. Remove or rewrite stale risk notes after the code has fixed them; do not leave historical warnings as if they were still live behavior.
3. Preserve exact state names, command names, and transition conditions so the document stays mechanically useful.
4. Keep `## 待讨论取舍` as the final section of the canonical document.
5. If the document path or classification changes, update `docs/README.md` in the same change.

## Validation Floor

1. Run focused tests for the touched state-machine paths.
2. Run broader relevant tests when the change crosses queue, routing, headless, or gateway boundaries.
3. Do not commit if the new state graph still contains a path where attach/use succeeds but the user has no valid next action.
4. Do not commit if a blocked state depends only on an async event with no watchdog or manual escape hatch, unless that risk is now explicitly documented and intentionally accepted.

## Scope Reminder

This skill is a pre-commit guardrail for remote surface state changes. It is not the general relay debugging workflow and it should not replace normal implementation or testing work. Use it to close the loop after state-related code changes are otherwise ready.
