# AGENTS

## Conversation Handshake Rule

For direct instructions from the repository owner/user in chat:

- First send a concise restatement of your understanding:
  - what the user is asking you to do
  - what your immediate execution approach will be
- Then proceed automatically with implementation.
- If the user does not reply after the restatement, treat it as implicit approval and continue.
- If the user sends correction/steer feedback, immediately adjust to that steer and continue from the corrected direction.

This rule is mandatory for all agents collaborating in this repository.

## Staged Execution Continuity Rule

When the user explicitly asks to execute work in stages (for example: "按阶段推进", "分阶段推进", "阶段式推进", "staged rollout"):

- Treat staged execution as a continuous delivery instruction, not a request for phase-by-phase confirmation.
- Complete all planned stages in the same task flow by default.
- Do not stop after finishing only one stage.
- Do not pause mid-way to ask whether to continue to the next stage.
- This rule applies regardless of whether the work is tracked by a GitHub issue.
- The only valid stop conditions are:
  - hard blocker (missing dependency, external outage, permission limitation)
  - newly discovered contradiction that makes continuing unsafe
  - explicit user interruption or redirection
- When a valid stop condition happens, report the concrete blocker and the exact next action to resume staged execution.

## Project Skill

For work on this repository's relay stack, use the project skill at `.codex/skills/relay-stack-playbook/`.

Trigger it for:

- `relayd` / `relay-wrapper`
- Feishu bot integration
- VS Code remote integration
- Codex app-server protocol changes
- `/list`, `/attach`, `/use`, queue, thread routing, missing reply, helper/internal traffic issues

For remote surface state-machine changes, also use `.codex/skills/remote-state-machine-guardrail/`.

Trigger it for:

- changes to remote-surface state-machine logic carriers, not merely because nearby keywords or files appear
- attach / detach / `/use` / `/follow` / `/new` state predicates or transition logic
- headless launch / resume / cancel / timeout / recovery state progression logic
- queue routing / dispatch mode / local pause-handoff decision logic
- request capture / prompt gate / modal gate / staged input / selection-flow enter-exit logic
- selected-thread / attached-instance / input-routing decisions that determine whether and where the next user action can proceed
- command-availability matrix logic, or any change that adds or removes remote surface states
- do not trigger it for pure copy, styling, logging, tests, or refactors that do not touch those logic carriers

For Feishu card UI navigation / callback state-machine changes, also use `.codex/skills/feishu-ui-state-machine-guardrail/`.

Trigger it for:

- changes to Feishu card state-machine logic carriers, not merely because card-related files or keywords appear
- card callback payload schema or parsing logic
- card owner / kind / action routing logic
- inline replace vs append-only decision logic
- command menu / selection prompt / request prompt card navigation logic
- `daemon_lifecycle_id`, old-card reject, lifecycle stamping, or callback freshness decision logic
- projector / gateway logic that determines whether an existing card can still act or what state mutation its callback performs
- do not trigger it for pure copy, styling, logging, tests, or refactors that do not touch those logic carriers

For GitHub issue pickup, triage, refinement, implementation, or closure, also use `.codex/skills/issue-workflow-guardrail/`.

Trigger it for:

- a GitHub issue number or URL
- requests to "handle", "complete", "triage", "refresh", or "close" an issue
- deciding whether an issue can be started immediately
- issue label/comment/body refinement
- blocked issue handling and clarification follow-up

For repository-local pull/build/local-daemon upgrades, also use `.codex/skills/local-upgrade/`.

Trigger it for:

- requests to "本地升级"
- `upgrade-local.sh`
- pull latest code, rebuild, and upgrade the locally installed daemon
- requests to trigger the built-in local upgrade transaction from a repo build
- Without explicit user approval in the current turn, do not automatically run repository-local upgrade flows such as `./upgrade-local.sh` or `codex-remote local-upgrade`, even if they seem like the natural next validation step.
- Natural-language repo requests such as `本地升级`, `debug 一下`, or “看看这个 repo 绑定实例状态” are repository tasks:
  - use the repo-bound helper path, not daemon slash commands
  - upgrades go through `./upgrade-local.sh`
  - status/debug HTTP requests go through `bash scripts/install/repo-target-request.sh admin ...` or `bash scripts/install/repo-install-target.sh --format shell`
  - do not satisfy those natural-language requests by sending `/upgrade ...` or `/debug ...` to the daemon currently carrying the chat
- Explicit slash commands such as `/upgrade`, `/upgrade local`, `/upgrade latest`, and `/debug` remain direct daemon actions on the daemon that received the slash command.

For repository-local safe push after local commits, also use `.codex/skills/safe-push/`.

Trigger it for:

- requests to "推送", "push", or "提交并推送"
- `git push` rejected because upstream/remote already moved ahead
- requests to automatically fetch/rebase/retest/push this repository's current branch

## Web Design Baseline

For Web page design, layout, copy, or interaction changes, follow:

- `docs/general/web-design-guidelines.md`

Trigger it for:

- `web/src/**` page or route changes
- `internal/app/daemon/adminui/**` page or copy changes
- setup / admin / install / onboarding / status page redesign
- any change that adds or removes user-visible sections, steps, cards, or default-exposed technical details

Default rules:

- Every page must work on both desktop and mobile
- User-visible copy should be written for ordinary users, not from a development or architecture point of view
- Single-page flows must not become long scrolling dumps; future-step or low-priority information should be hidden, deferred, folded, or split
- User-visible pages must not contain internal design-purpose text such as “设计目的” or “设计说明”

If a Web change intentionally needs to extend or revise these rules, update `docs/general/web-design-guidelines.md` in the same change.

## Documentation Convention

For lifecycle design/reference docs under `docs/`:

- Place each file under exactly one of `docs/draft/`, `docs/inprogress/`, `docs/implemented/`, `docs/general/`, or `docs/obsoleted/`.
- Every `docs/**/*.md` file must start with a visible metadata block directly under the title:
  - `Type`
  - `Updated`
  - `Summary`
- `Type` must match the directory name.
- If a document becomes obsolete, move it to `docs/obsoleted/` instead of leaving stale copies in place.
- When moving docs, update relative links and the index in `docs/README.md` in the same change.

## Core State Machine Document

The canonical remote surface state machine document is:

- `docs/general/remote-surface-state-machine.md`

For any change that modifies remote surface behavior or state transitions:

1. Implement and test first.
2. After the implementation stabilizes and before committing, reopen the canonical state machine document and update it to match the new behavior.
3. In that same pre-commit pass, explicitly audit for dead states, half-dead states, stale modal/UI states, silent route retargeting, and any transition that leaves the user without a clear next action.
4. If that audit reveals a bug-level issue, fix it and re-run the audit once more before committing.
5. Do not run this loop after every tiny edit; run it once near commit unless a major assumption changed mid-implementation.
6. If a remaining issue needs product tradeoff input rather than an engineering fix, append it to the end of `docs/general/remote-surface-state-machine.md` under `待讨论取舍` before discussing it.

Trigger boundary note:

- This loop is triggered by touching remote-surface state-machine logic carriers, even when the developer did not intend a product behavior change.
- It is not triggered by pure copy, styling, logging, tests, or refactors that leave the state predicates, transition conditions, command matrix, and routing decisions unchanged.

## Feishu UI State Machine Document

The canonical Feishu card UI state machine document is:

- `docs/general/feishu-card-ui-state-machine.md`

For any change that modifies Feishu card navigation, callback payloads, inline replace, or card freshness semantics:

1. Implement and test first.
2. After the implementation stabilizes and before committing, reopen the canonical Feishu UI state machine document and update it to match the current behavior.
3. In that same pre-commit pass, explicitly audit for stale cards that still mutate state, same-context navigation that unexpectedly appends, payload schema drift between projector and gateway, and any callback path that leaves the user with a clickable but already-dead card.
4. If that audit reveals a bug-level issue, fix it and re-run the audit once more before committing.
5. If the same change also affects attach / use / follow / `/new` / request-gate product semantics, run the core remote surface state-machine loop in the same pass.
6. If a remaining issue needs product tradeoff input rather than an engineering fix, append it to the end of `docs/general/feishu-card-ui-state-machine.md` under `待讨论取舍` before discussing it.

Trigger boundary note:

- This loop is triggered by touching Feishu card UI state-machine logic carriers, even when the developer did not intend a visible UX change.
- It is not triggered by pure copy, styling, logging, tests, or refactors that leave callback schema, routing decisions, freshness checks, and replace-vs-append decisions unchanged.

## GitHub Issue Workflow

For medium or large follow-up work in this repository:

- Before starting code-related work, if the local worktree is clean, first sync to the latest safe state of the current branch.
- Default rule for a clean worktree: do not start reading, assessing, or editing code against a known-stale local checkout.
- If the worktree is not clean, do not pull blindly; either finish, shelve, or explicitly treat the local changes as the current working context.
- Track it in GitHub Issues instead of a local `TODO.md` or similar scratch file.
- Tiny fixes that can be completed immediately in the same task do not need an issue; just implement them directly.
- This workflow applies whether the active issue was opened by the user, the repository owner, or any other contributor.
- The fixed mechanical entry points are:
  - `bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh prepare --issue <number>`
  - `bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh lint --issue <number>`
  - `bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue <number> [--comment-file path] [--close]`
- `full` workflow remains the default and keeps the existing behavior described below.
- `fast` workflow is an explicit opt-in shortcut for already-clear, single-stage, low-risk issue execution.
- User override phrases that force `fast` include:
  - `workflow:fast`
  - `fast path`
  - `快速处理`
  - `简化流程`
- User override phrases that force `full` include:
  - `workflow:full`
  - `full path`
  - `完整流程`
  - `标准 issue workflow`
- Default rule: use those commands for sync, claim, issue-shape checks, and cleanup instead of redoing raw `git` / `gh` sequences by hand each time. Keep model reasoning for semantic reassessment, planning, validation choice, and comment/body content.
- Default lifecycle is:
  1. sync local tracked files to the latest safe state
  2. reassess the issue against the latest code and current issue state
  3. refresh the issue body, labels, and status if needed
  4. only when the issue remains implementable, execute staged delivery
  5. validate every stage
  6. close the issue after the requested acceptance criteria are satisfied
- If the implementation plan changes materially after new code evidence, runtime evidence, or maintainer comments, update the active issue body before continuing.
- Do not leave an outdated staged plan only in comments when the issue body can be refreshed cheaply and accurately.
- Do not do a full open-issue sweep before every commit. Issue review happens when creating, picking up, or closing an issue, not as a mandatory pre-commit loop.
- For existing issues created before the standard, do not run a one-time bulk cleanup pass. Normalize them only when they become active.
- `fast` path boundaries:
  - use it only when the issue is already implementable, the scope is small, and one implementation stage is enough
  - keep `prepare` and `finish`
  - still validate the changed behavior
  - you may skip staged-plan authoring and mid-task body rewrites when no material plan change occurred
  - if the issue turns out broader, blocked, cross-cutting, or needs staged rollout after all, immediately fall back to the unchanged `full` path
- Use `processing` as a temporary single-worker claim while actively handling one issue in a turn:
  - after syncing local tracked files and before substantive issue assessment, first check whether the issue already has `processing`
  - if `processing` is already present, stop there and do not continue handling that issue in this turn
  - if `processing` is absent, add it before continuing
  - on every normal stop path for that issue in the current turn, remove `processing` before finishing:
    - stopping because the issue is not implementable yet
    - stopping after a state-transition update
    - stopping after implementation, validation, and close-out
  - if `processing` is accidentally left behind by an interrupted session, it may be cleared manually; do not add extra automatic recovery rules here

When creating or refreshing an issue, use this structure:

- Required sections:
  - `背景`
  - `目标`
  - `完成标准`
- Preferred sections when already known:
  - `范围`
  - `非目标`
  - `相关文档`
  - `涉及文件`
  - `建议范围`
- If `相关文档` or `涉及文件` are not known yet, they may be omitted temporarily rather than guessed.
- If `范围` or `非目标` are already clear, record them early to reduce later product ambiguity.

When picking up or re-assessing an active issue, follow this order:

1. First sync local tracked files to the latest safe state of the working branch.
   - Do not assess or code against stale local code.
   - If local changes prevent a safe sync, resolve that first or explicitly treat it as a blocker.
   - Prefer `issuectl.sh prepare` for this step; it blocks on tracked local changes, runs `git pull --ff-only`, fetches the issue snapshot, and claims `processing`.
2. Read the current issue body, current labels, the latest comments, and the current code.
3. Check whether `背景`, `目标`, and `完成标准` are present and specific enough.
   - An issue is not implementable yet if these minimum sections are still missing or too vague.
4. If `相关文档` or `涉及文件` can be identified cheaply from repo context, add them during that refinement pass.
5. If `范围` or `非目标` are already clear, add them.
6. If some information is still unknown, mark it as `待补充` or leave an explicit assumption comment; do not guess hidden requirements and present them as fact.
7. If original history or motivation cannot be reconstructed, do not invent it.
   - Record only the current confirmed background.
8. If the issue is still too broad after refinement, narrow it or split follow-up work into additional issues before implementation.

After reassessment, classify the issue into one of these states:

- `implementable now`
- `status:needs-investigation`
- `status:needs-clarification`
- `status:blocked`

State-transition rule:

- Compare the reassessed state with the issue's previously recorded actionable state.
- If the state changed in either direction, update the issue body and labels, leave the necessary concise evidence if applicable, run `issuectl.sh finish --issue <number> --skip-checks`, and stop there for this turn.
- If the state did not change but the issue is still not implementable, update the issue with any newly confirmed evidence, run `issuectl.sh finish --issue <number> --skip-checks`, and stop there for this turn.
- Only when the issue was already implementable and remains implementable after reassessment may implementation start immediately.

Issue labeling rule:

- Add at least one category label when applicable, for example:
  - `enhancement`
  - `bug`
  - `maintainability`
  - `testing`
  - `documentation`
- Add at least one scope label when applicable, for example:
  - `area:web`
  - `area:daemon`
  - `area:feishu`
  - `area:codex`
  - `area:runtime`
  - `area:wrapper`

Status-label rule for issues that cannot be started immediately:

- Use at most one of these labels at a time:
  - `status:needs-investigation`
  - `status:needs-clarification`
  - `status:blocked`
- Clear any stale status label when the issue moves to a different blocked state or becomes implementable again.
- Before implementation starts, keep comments focused on live collaboration only:
  - blocking questions
  - decision points
  - evidence that explains why work cannot safely start yet
- Do not use pre-implementation comments as a long-term archive dump; durable structure belongs in the issue body.

For staged delivery against an implementable issue:

1. Prefer staged delivery for medium or large work.
2. Before starting the first stage, write down the current staged plan in the active issue or a linked design doc referenced by the issue.
3. Before starting each later stage, re-read the relevant issue, design doc, and current code state, then re-evaluate whether the remaining plan is still correct.
4. Before each stage, re-run the repository skills that match the work in that stage, including the issue skill and any domain skill already required by the task.
5. If the plan, stage split, or best next step changes at any point, update the staged plan back to the issue before coding the next stage.
6. Unless a major assumption collapsed and the remaining execution direction would materially diverge, continue through all planned stages in the same task without pausing for confirmation.
7. Each stage must end with:
   - implementation
   - validation scoped to that stage
   - a local commit
8. Every stage requires sufficient testing.
   - Prefer tests and validation that exercise the changed behavior and runtime path, not only compilation or superficial smoke checks.
9. After each stage-end commit, immediately reassess how that completed work affects the next stage before continuing.
10. If implementation discovers a better stage split, update the plan first, then continue under the revised stages.

For explicit `fast` path execution against an implementable issue:

1. Keep the existing `full` workflow untouched; `fast` is an opt-in shortcut, not a rewrite of the default path.
2. Run `prepare` first and verify the issue is still implementable now.
3. Do not skip required issue sections, state checks, or validation of the changed behavior.
4. You may skip staged-plan authoring when the work is clearly single-stage.
5. You may skip issue body rewrites when the issue body is already accurate enough and no material plan change occurred.
6. Use a single implementation pass: implement, validate, commit, close out.
7. If new evidence shows the work is no longer single-stage or low-risk, switch back to the unchanged `full` workflow before continuing.

If implementation reveals another medium or large follow-up task, open a new issue for it instead of leaving a local TODO note behind.

When closing an issue, leave a short completion note that includes:

- what was implemented
- what was intentionally not changed, if that matters for future readers
- how it was validated
- the commit or PR reference
- any follow-up issue if remaining work was intentionally deferred
- Before finishing the turn, remove `processing`.
  - Prefer `issuectl.sh finish --issue <number> --comment-file <file> --close` so local mechanical checks, close-out, and `processing` release stay coupled.

## Git Push Rule

When a change is intentionally committed during task work:

- Push it to GitHub in the same turn by default.
- Exception: when the user explicitly requests staged local-only commits between phases, follow `GitHub Issue Workflow` and do not push until the staged rollout is complete or the user asks for a push.
- Exception: when applying `Clean Worktree Stop Rule` at a pause boundary and no push was requested, a local commit may be kept and pushed later.
- Do not leave a local-only commit behind unless one of the above exceptions applies.
- For the common happy path, prefer `./safe-push.sh` from the repo root instead of manually doing `fetch -> rebase -> retest -> push`.
- `./safe-push.sh` is intentionally narrow:
  - it requires a clean worktree
  - it fetches the target branch
  - if the remote branch moved ahead, it rebases onto it
  - after a successful rebase, it reruns tests, defaulting to `go test ./...`
  - after a successful rebase, it requires an explicit post-rebase audit before push
  - that audit must re-check:
    - whether the implementation direction still matches the intended plan
    - whether the implementation still matches the intended behavior after rebasing
  - if no drift is found, continue push
  - if drift is found, fix it first, then continue and finish
- If rebase conflicts or tests fail, stop and handle that manually; do not try to script conflict resolution into the helper.

## Clean Worktree Stop Rule

For repository tasks, when pausing or ending a turn with local modifications (tracked or untracked), always classify each change before stopping:

- `done-and-worth-commit`: the change is complete enough and valuable; commit it before stopping. Push is optional at this point unless the user requested immediate push.
- `temporary-in-progress`: the change is unfinished but intentionally kept for the next step; it may remain local.
- `useless-or-noise`: the change is not useful (experimental leftovers, accidental outputs, or stale scratch edits); remove it before stopping.

Default expectations:

- Do not leave mixed unknown leftovers in the worktree.
- If temporary changes are kept, they should be clearly intentional and scoped to upcoming work.

## Branch Restoration Rule

For repository-local operations that temporarily switch away from the current branch or ref:

- Before switching, record the starting branch or exact ref.
- If the task only needs a temporary branch change, treat returning to that starting branch/ref as part of the operation, not as optional cleanup.
- This applies in particular to:
  - local release or publish flows that need to check out a release branch
  - temporary `cherry-pick` work on another branch
  - cross-branch validation, comparison, or backport work that checks out another branch and then returns
- After the temporary operation finishes, return to the recorded starting branch/ref on every normal exit path, whether the operation succeeded or failed.
- Do not leave the repository parked on the temporary branch unless the user explicitly asks to stay there.

## File Length Gate Rule

For repository file-length enforcement:

- `bash scripts/check/go-file-length.sh` is a mandatory repository gate, not an advisory reminder.
- Do not bypass this gate with `git commit --no-verify`, by disabling hooks, or by treating an unrelated oversized file as acceptable background debt.
- If a commit is blocked by an existing oversized file, the default next step is to split or otherwise reduce the offending file until the check passes.
- Splitting for this gate must be structure-first, not line-count-first:
  - before moving code, read the whole oversized file and identify its main responsibility clusters, state ownership points, and external API surface
  - write a short split plan in the active issue or implementation notes that states target files and ownership boundaries
  - split by cohesive responsibility boundaries (for example: transport/parsing, orchestration/state transitions, rendering/projection, persistence/io), not by arbitrary function count or contiguous line chunks
  - keep behavior stable during the split unless behavior change is explicitly part of the same task, and avoid mixing unrelated refactors into a gate-driven split
  - each new file should have a clear purpose, stable naming, and minimal cross-file back-and-forth dependencies
- If fixing one oversized file reveals another existing oversized file, continue resolving the newly exposed blocker instead of bypassing the gate.
- A split is not complete just because the line limit passes; it is complete only after:
  - the affected package tests or equivalent validation pass
  - imports/dependencies reflect the intended ownership boundaries instead of accidental circular flow via helper leakage
  - any required design/state-machine docs are updated when logic ownership or lifecycle boundaries changed
- Do not leave the repository in a state where future commits still require skipping the file-length check.

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

## Structural Gap Rule

When the right fix depends on metadata, correlation handles, or lifecycle identity that current structs or APIs do not carry:

- Missing capability in the current structure is not, by itself, a reason to deny the direction or stop the work.
- Default first question: how should the structure change so the requirement can be expressed correctly?
- Treat that as evidence that the next step is usually a bounded structural change:
  - capture the metadata at the ingress/translation boundary
  - preserve it through the transport or control structs
  - apply policy at the owning product/runtime layer
- Prefer explicit metadata plumbing over heuristic reconstruction when the protocol or SDK already exposes an exact field.
- Prefer extending the structure to match the real requirement over forcing the requirement into today's narrower shape.
- When a medium-sized infrastructure stage is implementable but the final policy is still undecided, split the work:
  - stage 1: metadata, observability, stamping, correlation, tests
  - later stage: accept/drop/retry UX or policy decisions
- In issue triage, do not leave this kind of plan only in comments when it can be stated cleanly in the issue body or a follow-up issue.

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
- A remembered "helper thread id" is not a valid classifier for later turn/item traffic. Helper turns and normal user turns can share the same thread.
- Correlate helper thread/turn lifecycle only through protocol-level identifiers returned by the server, such as request `id -> result.thread.id` or `id -> result.turn.id`.
- If the real protocol provides an exact correlation handle, use it. Do not replace it with timing-based or adjacency-based guesses in production logic or mocks.

## Layer Ownership Rule

For wrapper/server protocol work:

- The wrapper is responsible for accurate translation and explicit annotation, not for product-side visibility policy.
- If a native lifecycle event is real app-server runtime traffic, prefer emitting it with canonical metadata such as `trafficClass` / `initiator` instead of silently swallowing it.
- Product decisions such as "pause queue", "render to Feishu", "hide helper traffic", or "update selected thread" belong in the server/orchestrator layer and must be tested there.
