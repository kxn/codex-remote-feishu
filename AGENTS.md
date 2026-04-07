# AGENTS

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

- `attach` / `detach` / `/use` / `/follow`
- headless launch, resume, cancel, timeout
- queue routing, dispatch mode, local pause/handoff
- request capture, prompt/card routing, selection flow
- `/new` or any change that adds or removes remote surface states

For GitHub issue pickup, triage, refinement, implementation, or closure, also use `.codex/skills/issue-workflow-guardrail/`.

Trigger it for:

- a GitHub issue number or URL
- requests to "handle", "complete", "triage", "refresh", or "close" an issue
- deciding whether an issue can be started immediately
- issue label/comment/body refinement
- blocked issue handling and clarification follow-up

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

## Issue Tracking Rule

For medium or large follow-up work in this repository:

- Track it in GitHub Issues instead of a local `TODO.md` or similar scratch file.
- Tiny fixes that can be completed immediately in the same task do not need an issue; just implement them directly.
- Default lifecycle is:
  1. create or refine the issue
  2. implement against the issue
  3. validate the result
  4. close the issue after the requested acceptance criteria are satisfied
- If the implementation plan changes materially after new code evidence, runtime evidence, or maintainer comments, update the active issue body before continuing.
- Do not leave an outdated staged plan only in comments when the issue body can be refreshed cheaply and accurately.
- Do not do a full open-issue sweep before every commit. Issue review happens when creating, picking up, or closing an issue, not as a mandatory pre-commit loop.

For existing issues created before the standard, do not run a one-time bulk cleanup pass.
Instead, normalize them when they become active:

1. When picking up an issue, first check whether it has enough information to implement safely.
2. The minimum implementable issue must clearly state:
   - `背景`
   - `目标`
   - `完成标准`
3. If those sections are missing or too vague, update the issue before coding.
4. If `相关文档` or `涉及文件` can be identified cheaply from the current repo context, add them during that refinement pass.
5. If some information is still unknown, mark it as `待补充` or leave an explicit assumption comment; do not guess hidden requirements and present them as fact.
6. If the issue is still too broad after refinement, narrow it or split follow-up work into additional issues before implementation.

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

If implementation reveals another medium or large follow-up task, open a new issue for it instead of leaving a local TODO note behind.

When closing an issue, leave a short completion note that includes:

- what was implemented
- what was intentionally not changed, if that matters for future readers
- how it was validated
- the commit or PR reference
- any follow-up issue if remaining work was intentionally deferred

Status-label rule for issues that cannot be started immediately:

- Use at most one of these labels at a time:
  - `status:needs-investigation`
  - `status:needs-clarification`
  - `status:blocked`
- Clear any stale status label when the issue becomes implementable again or moves to a different blocking state.
- Before implementation starts, keep comments focused on live collaboration only:
  - blocking questions
  - decision points
  - evidence that explains why work cannot safely start yet
- Do not use pre-implementation comments as a long-term archive dump; durable structure belongs in the issue body.

## Staged Delivery Workflow

For medium or large work that is intentionally split into multiple implementation stages:

1. Before starting the first stage, write down the current staged plan in the active issue or design doc.
2. Before starting each later stage, re-read the relevant issue, design doc, and current code state, then re-evaluate whether the remaining plan is still correct.
3. Before each stage, re-run the repository skills that match the work in that stage, including the issue skill and any domain skill already required by the task.
4. If the best next step changed, update the staged plan in the relevant issue or design doc before coding that stage.
5. If the plan change is material, update the active GitHub issue body rather than leaving the new plan only in comments.
6. Each stage must end with:
   - implementation
   - validation scoped to that stage
   - a local commit
7. Stage-end commits must not be pushed yet unless the user explicitly asks to push mid-rollout.
8. After each stage-end commit, immediately reassess how that completed work affects the next stage before continuing.
9. Continue through all planned stages in the same task without pausing for confirmation unless a major assumption collapsed and the remaining plan would likely be invalid.
10. If implementation discovers a better stage split, update the plan first, then continue under the revised stages.

## Git Push Rule

When a change is intentionally committed during task work:

- Push it to GitHub in the same turn by default.
- Exception: when the user explicitly requests staged local-only commits between phases, follow `Staged Delivery Workflow` and do not push until the staged rollout is complete or the user asks for a push.
- Do not leave a local-only commit behind unless the user explicitly asks not to push yet.

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
- A remembered "helper thread id" is not a valid classifier for later turn/item traffic. Helper turns and normal user turns can share the same thread.
- Correlate helper thread/turn lifecycle only through protocol-level identifiers returned by the server, such as request `id -> result.thread.id` or `id -> result.turn.id`.
- If the real protocol provides an exact correlation handle, use it. Do not replace it with timing-based or adjacency-based guesses in production logic or mocks.

## Layer Ownership Rule

For wrapper/server protocol work:

- The wrapper is responsible for accurate translation and explicit annotation, not for product-side visibility policy.
- If a native lifecycle event is real app-server runtime traffic, prefer emitting it with canonical metadata such as `trafficClass` / `initiator` instead of silently swallowing it.
- Product decisions such as "pause queue", "render to Feishu", "hide helper traffic", or "update selected thread" belong in the server/orchestrator layer and must be tested there.
