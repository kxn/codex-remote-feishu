# AGENTS

## Conversation Handshake (Always On)

- For direct user instructions, first send a short restatement:
  - what the user asked
  - what you will do immediately
- Then execute without waiting unless user explicitly asks to pause.
- If user gives correction/steer feedback, switch direction immediately.

## Trigger Accuracy Rule (Always On)

- Skill triggering uses **union matching**: trigger when **any** of these match:
  - user wording / command intent
  - touched logic carrier
  - touched file area
  - known symptom pattern
- Do not narrow trigger scope below prior behavior.
- If multiple skills match, use all relevant skills together.
- Exclusion notes (for example “pure copy/styling/logging/tests only”) apply only when you can confirm logic carriers are unchanged.

## Workspace Cleanliness Rule

For every new repository task in chat (not only GitHub issue workflow):

- Before starting substantive read/edit/build work, check current workspace cleanliness with `git status --short`.
- If the worktree is clean, proceed normally.
- If the worktree is not clean, do not silently continue with mixed context:
  - first classify existing local changes as either `same-task` or `different-task`
  - if `different-task`, stop and ask the user whether to:
    - commit/push them first
    - shelve them
    - or explicitly continue in dirty workspace
  - if `same-task`, explicitly state that assumption in chat and continue
- Do not mix unrelated edits into one commit by default.
- When the user asks to "先提交" or similar, complete that commit before starting additional implementation work.

## Staged Execution Continuity Rule

When the user explicitly asks staged rollout (for example: `按阶段推进`, `分阶段推进`, `阶段式推进`, `staged rollout`):

- Treat it as continuous execution by default.
- Complete all planned stages in one flow unless a real blocker appears.
- Do not pause after one stage to ask whether to continue.
- Treat a phase or stage as execution order only, not as a normal stopping boundary.
- For unsplit issue work, finishing `阶段 A / B / C` is never by itself a valid reason to stop.
- At the end of every phase, explicitly re-check:
  - is the issue already fully complete?
  - is there a hard blocker or real contradiction?
  - did execution hit a product decision gate?
  - does the issue now need a formal split before continuing?
- If all answers above are no, continue immediately into the next phase instead of stopping at the phase boundary.
- For repository or issue work, do not treat “local implementation is done” as a valid stopping point by itself.
- The staged flow is only complete when the normal tail work is also done for that task:
  - validation finished
  - required docs / issue state synced
  - commit completed when the change is worth keeping
  - push completed when repo policy says it should be pushed
  - issue closed when the issue is actually finished
- Valid stop conditions:
  - hard blocker (dependency/outage/permission)
  - newly discovered contradiction that makes continuing unsafe
  - explicit user redirection
- On stop, report blocker evidence and exact resume action.

## Skill Trigger Matrix

### `relay-stack-playbook`

Use `.codex/skills/relay-stack-playbook/` when working on:

- `relayd` / `relay-wrapper`
- Feishu bot inbound/outbound behavior
- VS Code remote integration
- Codex app-server protocol translation
- `/list`, `/attach`, `/use`, `/stop`
- queue/dispatch/thread routing/surface session issues
- helper/internal traffic classification issues
- “VS Code 有回复但飞书没回复” and similar missing-reply symptoms

### `remote-state-machine-guardrail`

Use `.codex/skills/remote-state-machine-guardrail/` for remote-surface state-machine logic carriers:

- attach/detach, `/use`, `/follow`, `/new` state predicates or transitions
- selected-thread / attached-instance / input-routing decisions
- queue routing / dispatch mode / pause-handoff gating
- headless launch/resume/cancel/timeout/recovery progression
- request-capture / prompt-gate / modal-gate / staged-input / selection-flow enter-exit
- command-availability matrix
- any change that adds/removes remote surface states

Do not trigger only for pure copy/styling/logging/tests/refactor with no logic-carrier change.

### `feishu-ui-state-machine-guardrail`

Use `.codex/skills/feishu-ui-state-machine-guardrail/` for Feishu card UI state-machine logic carriers:

- callback payload schema/parsing
- card owner/kind/action routing
- inline replace vs append-only decision
- command menu / selection prompt / request prompt navigation
- `daemon_lifecycle_id`, old-card rejection, freshness/lifecycle stamping
- projector/gateway decisions on whether an existing card can still mutate state

Do not trigger only for pure copy/styling/logging/tests/refactor with no logic-carrier change.

### `issue-workflow-guardrail`

Use `.codex/skills/issue-workflow-guardrail/` when task is centered on a GitHub issue:

- issue number or issue URL appears
- raw issue needs shaping before execution
- user asks to handle/complete/triage/refresh/close an issue
- need parent/child issue split, schedule table, or staged orchestration
- need result roll-up or next ready unit selection
- need implementable-now reassessment / blocked-state update
- need issue label/body/comment refinement

Mode override phrases:

- force `fast`: `workflow:fast`, `fast path`, `快速处理`, `简化流程`
- force `full`: `workflow:full`, `full path`, `完整流程`, `标准 issue workflow`

### `issue-verifier`

Use `.codex/skills/issue-verifier/` when issue work needs an independent validation pass:

- `验收`
- `独立验证`
- `对齐验证`
- `完成前复核`
- medium/large issue is otherwise ready to close and needs a read-only acceptance check

If the issue is still shaping, still missing execution closure, or still actively being implemented, stay on `issue-workflow-guardrail` first.

### `local-upgrade`

Use `.codex/skills/local-upgrade/` for repository-local daemon upgrade/debug routing:

- `本地升级`
- `upgrade-local.sh`
- pull latest + rebuild + upgrade local daemon
- local-upgrade transaction from repo build
- without explicit user approval in the current turn, do not auto-run repository-local upgrade flows

Natural-language repo requests (for example `本地升级`, `debug 一下`, `看下 repo 绑定实例状态`) are **repo tasks**:

- upgrade via `./upgrade-local.sh`
- status/debug via `bash scripts/install/repo-target-request.sh ...` or `bash scripts/install/repo-install-target.sh --format shell`
- do not satisfy those natural-language requests by sending daemon slash commands

Explicit slash commands (`/upgrade`, `/upgrade local`, `/upgrade latest`, `/debug`) stay as daemon-direct actions.

### `safe-push`

Use `.codex/skills/safe-push/` when pushing committed changes:

- user says `推送` / `push` / `提交并推送`
- `git push` rejected because remote advanced
- user asks fetch/rebase/retest/push flow

Prefer `./safe-push.sh` for happy-path push.

### `issue-doc-sync`

Use `.codex/skills/issue-doc-sync/` when syncing closed GitHub issues back into `docs/`.

## Web Design Baseline

For web page design/layout/copy/interaction changes, follow:

- `docs/general/web-design-guidelines.md`

Trigger area:

- `web/src/**`
- `internal/app/daemon/adminui/**`
- setup/admin/install/onboarding/status page redesign
- any change that adds/removes user-visible sections, steps, cards, or default-exposed technical details

Baseline requirements:

- desktop + mobile both work
- copy is user-facing, not architecture-facing
- avoid long dump pages; defer/fold/split lower-priority info
- do not expose internal design-purpose text to end users
- if baseline rules are intentionally changed, update `docs/general/web-design-guidelines.md` in the same change

## Feishu Card Constraints Baseline

For any design or implementation that touches Feishu cards, message-card patching, CardKit streaming, or card interaction pacing, consult:

- `docs/general/feishu-card-api-constraints.md`

Trigger area:

- `internal/adapter/feishu/**`
- `internal/core/orchestrator/**` when change alters Feishu-surface card payloads, update cadence, or interaction composition
- `docs/general/feishu-card-ui-state-machine.md`
- any design/doc discussing single-turn cards, live cards, approval cards, request cards, or plan/progress/final-card aggregation on Feishu

Baseline requirements:

- design against official hard limits first: card size, element count, per-message/per-card update qps, callback SLA, streaming/interaction switching
- do not assume one giant card is safe without explicit size budget and degradation path
- if a design may exceed limits, specify fallback up front: truncate, summarize, paginate, split card, or spill to text/file/link
- when relying on a numeric Feishu platform limit in code or product design, re-check the latest official docs and sync `docs/general/feishu-card-api-constraints.md` if the baseline changed

## Feishu Menu Card Baseline

For any design or implementation that touches Feishu command menu cards, launcher-to-business handoff, or menu callback replace/append behavior, consult:

- `docs/general/feishu-menu-card-usage-guidelines.md`
- `docs/general/feishu-card-ui-state-machine.md`

Trigger area:

- `internal/core/control/feishu_ui_lifecycle.go`
- `internal/core/orchestrator/service_page_view.go`
- `internal/core/orchestrator/service_feishu_ui_context.go`
- `internal/core/orchestrator/service_workspace_page.go`
- `internal/app/daemon/app_ingress.go`
- menu-related Feishu callback/view docs under `docs/general/`

Baseline requirements:

- menu actions must be classified through unified frontstage contract (`keep` / `enter_owner` / `enter_terminal`)
- menu card must stay launcher-only; business execution state belongs to owner cards
- do not reintroduce submission-anchor fallback, bare command continuation fallback, or legacy menu substrate
- any new menu item must include contract mapping, handoff behavior, and regression tests for menu handoff + inline replacement

## Feishu Card Content Context Baseline

For any design or implementation that touches Feishu card text contracts, markdown/plain_text split, or card text render helpers, consult:

- `docs/general/feishu-card-content-context-guidelines.md`

Trigger area:

- `internal/adapter/feishu/**`
- `internal/core/control/**` when adding or changing Feishu card DTO text fields
- `internal/core/orchestrator/**`
- `internal/app/daemon/**` when constructing Feishu-facing summary/status/message/notice/catalog text
- any design/doc discussing Feishu card markdown contracts, `plain_text`, `FeishuCardTextSection`, `SummarySections`, or `renderSystemInlineTags(...)`

Baseline requirements:

- external or dynamic text must not be precomposed into raw markdown upstream
- adapter owns the final markdown/plain_text split and Feishu-specific tags
- `renderSystemInlineTags(...)` and similar helpers are local formatting helpers, not general sanitizers
- new card text defaults to structured/plain-text carriers such as `FeishuCardTextSection` and `SummarySections`
- when a new structured path replaces an old markdown contract, remove the old path instead of leaving permanent legacy/fallback coexistence
- add regression tests that prove dynamic text stays out of markdown, or explicitly justify the remaining markdown contract

## Documentation Convention

For lifecycle/reference docs under `docs/`:

- place docs in exactly one lifecycle dir:
  - `docs/draft/`
  - `docs/inprogress/`
  - `docs/implemented/`
  - `docs/general/`
  - `docs/obsoleted/`
- every `docs/**/*.md` starts with visible metadata block below title:
  - `Type`
  - `Updated`
  - `Summary`
- `Type` must match directory lifecycle
- obsolete docs move to `docs/obsoleted/`
- when moving docs, update links and `docs/README.md` in same change

## State-Machine Doc Sync (Pre-commit)

Canonical docs:

- remote surface: `docs/general/remote-surface-state-machine.md`
- Feishu card UI: `docs/general/feishu-card-ui-state-machine.md`

When corresponding logic carriers changed:

- implement + test first
- run matching guardrail skill before commit
- sync canonical doc to implemented behavior
- audit dead/half-dead/stale-action states
- if bug-grade issue found, fix + retest in same pass
- if unresolved product tradeoff remains, append to `待讨论取舍`

## GitHub Issue Workflow (Policy)

- For medium/large issue work, use issue workflow skill and its fixed `prepare/lint/finish` entry points.
- Raw issues are allowed to start rough; first shape them to at least research closure before trying to implement.
- If an issue was opened by an external reporter, do not rewrite their original issue body into the repository's internal workflow template.
- For an external reporter issue, first create a new internal execution issue; use that internal issue as the workflow-managed unit for shaping, execution snapshots, staged plans, and close-out.
- If later split is needed, split under the internal execution issue rather than turning the external reporter issue itself into the parent scheduler.
- Treat the original external reporter issue as a communication surface: keep the original description, link the internal execution issue both ways, and sync results back there by comment.
- By default, close only the internal execution issue; do not auto-close the original external reporter issue unless the user explicitly asks for that policy.
- Do not start code assessment against known-stale checkout when worktree is clean.
- Tiny fixes that can be finished immediately do not require opening/normalizing an issue.
- Before handing a subtask to a worker, make sure the active child issue is an information closure or a stable closure index for execution.
- Prefer parent issue + child issue orchestration when one issue would otherwise mix multiple goals, validation surfaces, or weakly related code areas.
- Workflow-managed active issues should carry exactly one explicit workflow status label; use `status:implementable-now` for ready-to-start work instead of encoding readiness as “no status label”.
- When an issue is implementable and not truly single-stage, keep `建议范围`, `实现参考`, `检查参考`, and `收尾参考` current in the issue body.
- For multi-stage or multi-turn issue work, keep a durable execution snapshot in the issue body or linked design doc, including at least `当前执行点`, `下一步`, `最后一致状态`, `未完成尾项`, and `恢复步骤`.
- On resume, do not continue from chat memory alone. Re-read the execution snapshot and verify it against the current code and latest issue state before acting.
- For unsplit implementable issues, a recorded phase/stage is execution sequencing only; it does not create a default stop point.
- After each completed phase on an unsplit issue, explicitly decide only among:
  - issue is actually complete
  - blocked / contradiction / product gate
  - needs formal split before more coding
  - continue into the next phase now
- Do not stop merely because the current phase ended.
- When issue work uncovers a small, non-blocking, low-priority follow-up that is not worth a standalone issue, record it under a dedicated `低优先级待办` section in the active issue body instead of leaving it only in chat.
- If implementation uncovers a red inconsistency that changes goals, acceptance, dependencies, or sibling issue assumptions, stop local patching and return the result to the orchestrating issue for replanning.
- If issue work reaches a real product decision gate, do not guess. Add a dedicated `待决策` or `产品待拍板` section with the minimal decision packet, ask only for the smallest blocking decision, then resume after that decision is synced back into the issue body.
- For medium/large finished issues, default to an independent verifier pass before close-out; only skip when the user explicitly waives it or the task is explicitly `workflow:fast`.
- If a child issue has a parent issue, do not `finish --close` it until the parent has received a durable roll-up of the child result.
- If a parent issue is being closed, make sure its total view includes child roll-up state, verifier state, and current close judgment before `finish --close`.
- If an older issue is resumed under the current workflow, do not let it enter close-out with a legacy contract; first add the missing current workflow fields that the close gate depends on.
- Before `finish`, explicitly re-check whether durable knowledge changed enough to require syncing the issue body, linked docs, state-machine docs, or repo workflow guidance.
- For issue work requested as `处理`, `完成`, or staged rollout, do not stop after local code/test completion while any of these remain unfinished without a real blocker:
  - commit
  - push
  - final `finish`
  - issue close when acceptance is satisfied
- “I already implemented it locally” is not a sufficient reason to leave an issue open or leave commits unpublished.

## Commit / Push / Branch Policy

- If repository work is resolved and verified, do not end the turn with uncommitted changes unless the user explicitly wants local-only uncommitted state.
- If you intentionally commit during task work, push in the same turn by default unless user asked local-only staging.
- Do not end a repository task with local commits left unpushed unless one of these is true:
  - the user explicitly asked to keep it local-only
  - the branch is explicitly a temporary local experiment branch
  - push is genuinely blocked by conflict, failing post-rebase validation, permission, or outage
- If stopping in a local-only state, explicitly report:
  - `LOCAL-ONLY`
  - current branch
  - current `HEAD`
  - why it was not pushed
  - the exact next action needed to publish it
- Before treating a repository task as complete, re-check both:
  - `git status --short`
  - whether local `HEAD` is ahead of its upstream
- For issue work, also re-check whether the issue itself is still open only because of process tail work; if so, finish that tail work instead of stopping.
- For temporary branch/ref switches, record start ref and return on normal exit unless user explicitly says stay.

## File Length Gate Policy

- `bash scripts/check/go-file-length.sh` is mandatory; do not bypass with `--no-verify` or equivalent.
- If blocked by oversized files, perform structure-first split and keep behavior stable unless behavior change is in scope.

## Proxy / Wrapper Policy

- For local tests/debug against localhost, unset proxy env first:
  - `unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy`
- Wrapper exception:
  - `relay-wrapper` itself should run without proxy for local relay communication
  - when launching `codex.real`, restore captured proxy env for child process

## External Command Launch Policy

- Production Go code must not call `exec.Command` or `exec.CommandContext` directly.
- Use `internal/execlaunch` for new external process creation so Windows no-console defaults stay centralized.
- If a caller needs extra `SysProcAttr`, detached, or process-group behavior, layer it on top of `execlaunch.Prepare(cmd)` instead of replacing the shared defaults.

## Debugging / Ownership Guardrails

- Stateful bugs are evidence-first: collect full-path runtime evidence before patching.
- Do not classify helper/internal traffic using thread-local/timing heuristics; use protocol correlation ids.
- Wrapper owns accurate translation + explicit annotation; product visibility policy belongs to server/orchestrator layer.
- Config migration/install writes must preserve existing credentials unless explicit destructive reset flow is defined.
- For service lifecycle ops, do not run overlapping `stop/start/restart/bootstrap` on same daemon.
