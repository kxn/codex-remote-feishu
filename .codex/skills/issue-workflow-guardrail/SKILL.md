---
name: issue-workflow-guardrail
description: "Use when handling a GitHub issue for this repository, including raw issue shaping, implementability reassessment, parent/child issue orchestration, durable execution snapshots, product decision-gate handoff, staged execution, result roll-up, and verifier handoff. Run the fixed prepare/lint/finish workflow, keep the issue body current, and stop when the actionable state changed."
---

# Issue Workflow Guardrail

Use this skill whenever the task is centered on a GitHub issue in this repository.

Examples:

- the user gives an issue number or URL
- the issue is still raw and needs shaping before coding
- the user asks to complete, triage, refine, or close an issue
- the issue is large enough to need parent/child split or schedule management
- multiple worker results must be rolled back into a mother issue
- the issue may be blocked, underspecified, or waiting on clarification
- the issue may have been opened by the user or by someone else

Do not run a one-time cleanup pass over old issues. Normalize an issue only when it becomes active.

For medium/large issue work, treat [docs/general/issue-orchestration-workflow.md](../../../docs/general/issue-orchestration-workflow.md) as the durable process baseline and use this skill as the operational entrypoint.

If repo-root `.codex/private/issue-orchestration-private.md` exists, read it after the public workflow doc and treat it as a local-only augmentation layer for orchestration heuristics, split quality, resume discipline, and product-decision timing. Do not assume that file exists in other clones.

## Orchestration Model

Use this skill as the repository's main issue orchestrator:

- external reporter issue
  - preserve the reporter-owned body as the public communication record
  - before normalization or execution, create a new internal execution issue linked back to the external issue
  - use the internal issue as the workflow-managed unit for shaping, staged plans, snapshots, and close-out
  - if more splitting is needed later, split under the internal issue instead of rewriting the reporter-owned issue into a parent scheduler
- raw issue
  - shape the issue into a stable problem statement
  - decide whether it only has research closure or is ready for execution closure
- parent issue
  - hold the overall goal, schedule table, dependency order, and roll-up status
  - prefer this mode when one issue would otherwise mix multiple goals or validation surfaces
- child issue
  - treat as the default worker unit
  - do not hand it to implementation until it is an execution closure or a stable closure index
- unsplit direct-execution unit
  - when an issue stays unsplit, the active issue itself becomes the current worker unit
  - direct execution is not a bypass; it inherits the same worker-boundary, snapshot, product-gate, and verifier-decision rules
- execution snapshot
  - keep a durable current execution point and resume contract in the issue body or linked design doc
- product decision gate
  - when execution reaches a real product tradeoff, stop automation and hand back a minimal decision packet instead of guessing
- verifier handoff
  - when a medium/large issue is effectively complete, hand it to `$issue-verifier` for an independent read-only pass before close-out

Prefer these closure levels:

- `research closure`
  - enough information to decide whether to proceed, split, or keep investigating
- `execution closure`
  - enough information for a worker to implement without rebuilding wide context

If the active issue only reaches research closure, do not force direct implementation. Shape, split, or stop with the issue state updated.

## External Reporter Flow

When the active GitHub issue was opened by someone else and still acts as the public bug report or feature request:

1. do not rewrite the original reporter-owned body into the internal workflow format
2. create a new internal execution issue first
3. link the two issues both ways
4. put all workflow structure on the internal issue:
   - background / goal / acceptance
   - execution decision
   - staged plan
   - execution snapshot
   - implementation / check / finish context
5. use the original external issue only for clarification, evidence requests, and final result comments
6. close the internal execution issue when its close gates pass
7. do not auto-close the original external issue; leave a concise completion comment there instead

## Split and Roll-up Rules

Split before coding when any of these are true:

- the issue mixes multiple weakly related goals
- the required background is no longer a single coherent closure
- different parts need substantially different validation surfaces
- the work is naturally parallelizable

If you decide not to split, treat the active issue as the current worker unit and record that decision durably before coding.

For a parent issue, keep the body or a linked design doc current with:

- split structure
- recommended order
- dependency edges
- parallel groups
- current closure level for each unit
- next recommended ready unit

Roll results back into the parent issue whenever:

- a worker finishes a child issue
- a child issue changes the expected next stages
- new findings invalidate the previous split or dependency assumptions

Do not leave stage changes only in chat when they materially affect later execution.

## Execution Decision Record

After `prepare` succeeds and before coding starts, write or refresh an execution decision record.

This record must say at least:

- whether the issue is being split
- if not split, why the active issue can safely act as a single worker unit
- what the current worker unit is
- whether an independent verifier pass is expected before close-out
- if verifier is not planned, why skipping it is acceptable for this run

Put this record in the active issue body, the parent issue, or a linked design doc.
Do not leave it only in chat.

## Durable Execution Snapshot

Do not rely on live chat context as the only execution memory.

For medium/large issue work, maintain a durable execution snapshot in the active parent issue, child issue, or its linked design doc.

The snapshot should contain at least:

- current stage
- current execution point
- done
- next step
- current blocker
- recently changed assumptions
- last known-good consistent state
- unfinished tail work
- resume steps

Update the snapshot at least:

- at the end of every stage
- before any normal stop path
- before handing work to another worker
- after a red inconsistency
- before and after a product decision gate

On resume, never continue from memory alone. Re-read the snapshot, linked closure material, and current code, then confirm the recorded next step is still valid. If not, refresh the snapshot first and only then continue.

For an unsplit direct-execution unit, a recorded stage or phase is execution sequencing only, not a default stopping point.
When a stage ends, immediately decide among exactly these outcomes:

- the overall issue is actually complete
- a real blocker / contradiction / product decision gate now prevents safe continuation
- the issue must be formally split before more implementation
- continue directly into the next stage

Do not stop merely because `phase A` or another recorded stage finished.

## Worker Boundary

Workers include both child issues and an unsplit active issue doing direct execution.

Workers own execution inside the current issue closure, not replanning outside it.

Use this practical rule:

- green inconsistency
  - fix locally when goals, acceptance, dependencies, and sibling assumptions stay unchanged
- yellow inconsistency
  - do one bounded investigation
  - continue only if the result still stays inside the current closure
- red inconsistency
  - stop local implementation
  - update the issue with the contradiction
  - return control to the orchestrating issue instead of repeatedly hacking through a broken assumption

Common red signals:

- goal or acceptance would need to change
- dependency order would need to change
- sibling issue assumptions are now invalid
- the issue no longer forms a stable execution closure
- the unsplit active issue no longer forms a stable single-worker closure

## Product Decision Gate

Not every red inconsistency is purely technical.

When execution hits a real product decision, do not keep pushing by guessing product intent.

Treat it as a decision gate when any of these are true:

- user-visible semantics would change depending on the choice
- interaction or UX tradeoffs now matter to acceptance
- a technical limitation forces a product compromise
- multiple choices are implementable, but only product intent can decide the right one

At a decision gate:

1. stop autonomous implementation
2. update the active parent issue or current issue with a dedicated `待决策` or `产品待拍板` section
3. compress the problem into a minimal decision packet
4. ask only for the decision that is actually needed
5. after the user answers, sync the chosen direction back into the issue body before resuming work

The minimal decision packet should contain:

- trigger
- current constraint or evidence
- mutually exclusive options
- impact of each option
- recommended option
- exact decision needed
- affected child issues, stages, or validation surfaces

Do not dump the whole project context back onto the user. The goal is to ask the smallest question that safely unblocks the workflow.

## Verifier Hook

Use `$issue-verifier` when:

- implementation is done or nearly done
- acceptance looks satisfied
- you want an independent pass before closure

The verifier pass is a role boundary, not just a longer self-review. Default to read-only verification unless the user explicitly asks for fixes as part of the same step.
Even when the decision is “no verifier for this run”, make that an explicit recorded decision before close-out. Direct execution is not exempt.

## Workflow Modes

`full` remains the default and keeps the existing workflow behavior unchanged.

`fast` is an explicit opt-in shortcut for already-clear, single-stage, low-risk issue work.

Treat these user phrases as a forced mode override:

- force `fast`
  - `workflow:fast`
  - `fast path`
  - `快速处理`
  - `简化流程`
- force `full`
  - `workflow:full`
  - `full path`
  - `完整流程`
  - `标准 issue workflow`

If the user does not force `fast`, keep using the unchanged `full` flow.

## Fixed Entry Points

Default to the bundled wrapper instead of redoing raw `git` / `gh` sequences by hand:

```bash
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh prepare --issue <number>
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh lint --issue <number>
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh close-plan --issue <number>
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue <number> [--comment-file path] [--close]
```

When you intentionally want the helper output to match the chosen workflow mode, pass:

```bash
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh prepare --issue <number> --mode fast
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh lint --issue <number> --mode fast
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh close-plan --issue <number> --mode fast
```

`--mode full` keeps the legacy behavior.

What each command owns:

- `prepare`
  - blocks on tracked local changes before sync
  - runs `git pull --ff-only`
  - fetches the live issue snapshot from GitHub
  - claims `processing` when available
  - can reclaim a stale `processing` claim after the configured stale window
  - returns non-ready when a mature state label such as `status:needs-plan` or `status:implementable-now` still lacks its required workflow contract
  - writes a reusable snapshot JSON under `.codex/state/issue-workflow/`
- `lint`
  - checks required issue sections
  - checks status/category/scope label shape
  - fails when `status:needs-plan` or `status:implementable-now` is missing its required staged-plan / execution contract
  - fails when the execution snapshot says only close-out tail work remains but `当前执行点` / `下一步` still point at more implementation or validation
- `finish`
  - runs the fixed local mechanical checks
  - can post a comment, close the issue, and release `processing`
- `close-plan`
  - dry-runs the issue-side close gates
  - reports whether close is ready
  - returns explicit next actions for verifier / parent roll-up / parent summary / legacy contract blockers

Use these commands at fixed times:

1. Before substantive issue assessment, run `prepare`.
2. After body or label edits, run `lint`.
3. Before `finish --close`, run `close-plan`.
4. Before any normal stop path for the issue, run `finish`.

Important:

- For an implementable issue, “local code is written and tests passed” is not by itself a normal stop path.
- A stale `processing` label is a recoverable lease, not a durable lock. Reclaiming it only unlocks resume; it does not prove the issue is ready for close-out.
- Unless the user explicitly asked for local-only staging, continue through the routine tail work as part of the same issue flow:
  - commit the finished change
  - push it when repo policy says pushes should happen
  - post the final `finish` comment
  - close the issue when acceptance is satisfied
- Only stop short of that tail work when there is a real blocker or the user explicitly redirects you.

## GitHub CLI Compatibility

In this repository, do not rely on bare `gh issue view <number>` for issue reads.

- The default `gh issue view` path may fail with a GraphQL error tied to deprecated classic Projects fields such as `repository.issue.projectCards`.
- Prefer `gh issue view <number> --json ...` when you need structured issue data.
- Before using unfamiliar `gh ... --json` fields, prefer:

```bash
bash scripts/dev/gh-json-fields.sh --check number,title,state issue view <number>
```

- If you only need raw issue contents or `--json` is still inconvenient, use REST directly:

```bash
gh api repos/<owner>/<repo>/issues/<number>
```

- Do not misclassify this failure as an issue-content problem; it is usually a `gh` query compatibility problem.

Only spend extra reasoning on the parts the scripts cannot decide:

- whether the issue is actually implementable now
- whether the latest comments override the body
- how to refine the body content
- how to split staged delivery
- what tests are sufficient
- what the final completion or blocking comment should say

For deterministic repo facts, also prefer the bundled helpers:

```bash
bash scripts/dev/worktree-facts.sh
bash scripts/dev/resolve-repo-path.sh docs/general/issue-orchestration-workflow.md
```

Do not rerun the same deterministic failing command unchanged; first change the input or choose the right helper.

## Read Order

After `prepare` succeeds, read in this order:

1. `docs/general/issue-orchestration-workflow.md`
2. this skill file
3. the current issue body
4. linked design doc or closure index when present
5. current labels
6. the latest comments
7. the current code

Reason: `prepare` may have pulled newer local workflow guidance, so do not rely on a previously loaded copy of the process rules.

If later comments conflict with the body, treat the latest maintainer or user comment as the current direction. Update the body if that can be done cheaply and accurately.

If a durable execution snapshot exists, treat it as the default restart point, but still verify it against the current code before acting.

## Body vs Comment

Use the issue body for durable structure:

- background
- goal
- scope or non-goals
- related docs
- related files
- acceptance criteria
- execution decision (`是否拆分`, `当前执行单元`, verifier plan, and why)
- staged plan (`建议范围`) once the issue enters `status:needs-plan`, even if the current plan only has one execution unit
- execution snapshot (`当前执行点`, `恢复步骤`, and related fields) when work spans multiple stages or turns
- implementation context (`实现参考`)
- check context (`检查参考`)
- low-priority deferred follow-ups in a dedicated `低优先级待办` section when they are too small to justify a standalone issue
- finish / knowledge-sync context (`收尾参考`)

If the work started from an external reporter issue, keep this durable structure on the internal execution issue body, not on the original reporter-owned issue body.

Use comments only for live collaboration:

- blocking questions
- decisions that need a reply
- concise evidence that explains why work cannot safely start
- a short completion note when closing

Before implementation starts, do not use comments for long-term archive notes, process logs, or large summaries that belong in the body.

## Refinement Rules

When picking up or re-assessing an issue:

1. Check whether `背景`, `目标`, and `完成标准` are present and specific enough.
   - The issue is not implementable yet if these minimum sections are still missing or too vague.
2. If related docs or files can be identified cheaply from repo context, add them.
3. If scope or non-goals are already clear, add them.
4. If original history or motivation cannot be reconstructed, do not invent it.
   - Record only the current confirmed background.
   - Mark missing original context as `待补充` when needed.
5. If the issue is still too broad, narrow it or split follow-up issues before implementation.
6. If staged implementation is expected, write the current staged plan into the issue body before coding.
   - For larger work, also decide whether this issue should stay single-stage, become a parent issue, or be split into child issues.
7. Once the issue is implementable, decide and record whether it remains an unsplit single-worker issue or should become a parent/child split, and record the current verifier plan.
8. Once the issue is implementable, fill or refresh `实现参考`, `检查参考`, and `收尾参考`.
   - `实现参考`: recommended cut, key docs/files, current preferred solution, confirmed constraints
   - `检查参考`: risky flows, regression points, exact docs/tests to re-check
   - `收尾参考`: likely knowledge write-back targets such as issue body, linked design docs, docs/general, state-machine docs, AGENTS, or repo skills
   - For multi-stage or multi-turn work, also create or refresh the execution snapshot instead of relying on chat memory.
   - If a product decision gate already looks likely, prepare a `待决策` section early instead of waiting until implementation is confused.
9. If later investigation or implementation changes the staged plan, execution decision, or any execution-context section materially, update the issue body before continuing.
10. If work uncovers a small, non-blocking, low-priority follow-up that is not worth a standalone issue, append it to `低优先级待办` in the active issue body instead of leaving it only in chat.
   - Keep entries concise and actionable.
   - Include what is deferred and why it stayed in-body instead of becoming its own issue.
   - Use this section as the canonical source for later backlog harvest.

## Reassessment Decision

After refining against the latest code, classify the issue into one of these states:

- `implementable now`
- `needs investigation`
- `needs plan`
- `needs clarification`
- `blocked`

## State-Transition Rule

Compare the reassessed state with the issue's previously recorded actionable state.

- If the state changed in either direction, update the issue body, labels, and concise evidence as needed, then run `finish --issue <number> --skip-checks` and stop there for this turn.
- If the state did not change but the issue is still not implementable, update the issue with any newly confirmed evidence, then run `finish --issue <number> --skip-checks` and stop there for this turn.
- Only when the issue was already implementable and remains implementable after reassessment may coding start immediately.
- Do not code directly from `needs investigation` or `needs plan`; first update the issue until `status:implementable-now` and `lint` are both clean.
- Even on that path, write or refresh the execution decision record before coding.
- The minimum start sequence is: `prepare` -> re-read workflow doc and skill -> refresh `执行决策` -> refresh snapshot when applicable -> `lint` -> code.

## Status Labels

Workflow-managed issues should carry exactly one explicit workflow status label.

Apply exactly one of:

- `status:implementable-now`
  - use only when the issue has enough context to start implementation safely, including a written `建议范围`, `执行决策`, and execution context sections
- `status:needs-investigation`
  - use when the code or runtime path must be researched before safe implementation
- `status:needs-plan`
  - use when technical investigation is sufficient, but the execution plan has not yet been durably written back
- `status:needs-clarification`
  - use when product intent, user expectation, or acceptance criteria are still unclear
- `status:blocked`
  - use when an external dependency, upstream change, or awaited decision prevents progress

Do not encode “ready to implement” as the absence of a status label.

Remove stale status labels when the issue moves to a different workflow state.

## Blocking Comment Rules

When work cannot start, leave one concise comment that contains:

- current blocking state
- what was checked
- the exact missing question, decision, or dependency
- what reply or action would unblock the issue

Keep it short and actionable. Do not restate the full issue body.
If the blocker is a product decision gate, the comment should point to the in-body `待决策` section instead of duplicating all options in the comment.
Before you stop on this path, prefer `finish --issue <number> --comment-file <file> --skip-checks` so `processing` is released mechanically.

## Implementation Rules

If the issue was already implementable and still is after reassessment:

- do not leave a ritual “starting work” comment
- implement against the refined issue
- if the issue remains unsplit, explicitly treat it as the current worker unit instead of as a bypass path
- if the issue is actually serving as a parent issue, do not force coding in place; first refresh split/order/next-unit selection
- before each implementation stage, re-read the issue body, latest comments, current code state, and `实现参考`
- before each implementation stage, re-read the execution decision record and confirm it still matches reality; if not, update it before coding
- before each implementation stage, confirm the execution snapshot still matches reality; if not, update it before coding
- before each implementation stage, re-run any repository skills already required by the task so the next step is based on current guidance
- after each completed stage on an unsplit issue, explicitly run the stage-end check:
  - is the whole issue already complete?
  - is there a hard blocker / contradiction / product gate?
  - must the issue now be split before further coding?
  - if none of the above, continue immediately into the next stage
- before validation/check work, re-read `检查参考`
- if the best next stage or any execution-context section changed materially, update the issue body first instead of leaving the new plan only in a comment
- prefer staged delivery for medium or large work
- write the current staged plan into the issue or a linked design doc before stage 1
- update the staged plan back to the issue whenever the plan or stage split changes
- apply the same green/yellow/red worker-boundary rules to unsplit direct execution; do not keep coding through a red inconsistency
- if implementation hits a product decision gate, stop and return a minimal decision packet instead of silently picking one branch
- when you intentionally defer a tiny follow-up that is not worth a new issue, record it under `低优先级待办` before moving on
- continue through all planned stages in the same task unless a major assumption collapsed and the remaining direction would materially diverge
- every stage must include sufficient validation, not only compilation or superficial smoke checks
- each stage should end with implementation, stage-scoped validation, a refreshed execution snapshot, and a local commit
- when the overall issue is finished, do not stop at “last stage implemented locally”; continue through publish/close-out work in the same turn unless blocked
- for medium/large issue work, default to an independent verifier pass before close-out; only skip when the user explicitly waived it or the task is explicitly `workflow:fast`
- before close-out, record what local validation ran, whether verifier ran, and why any verifier skip was acceptable
- before `finish --close`, run `close-plan` and clear every failing issue-side close gate first
- if the current issue is a child issue, make sure its result has been durably rolled back into the parent issue before `finish --close`
- if the current issue is a parent issue, make sure its total view already includes child roll-up state, verifier state, and current close judgment before `finish --close`
- if the current issue is an older issue that predates the current workflow contract, rehab the missing parent/child link or close-out fields before attempting `finish --close`
- posting a “locally complete” comment is not an acceptable substitute for commit/push/close when the user asked to complete the issue
- validate the result
- before any normal stop path, re-read `收尾参考` and decide whether durable knowledge changed enough to require write-back
- update any affected design or state-machine document required by repo rules

## Finish Knowledge Write-back Rules

Before `finish`, explicitly decide whether this work changed durable knowledge:

1. Update the issue body or linked design doc when confirmed facts, stage split, or recommended execution path changed.
2. Update `docs/general/` or other canonical docs when user-visible behavior, contracts, state transitions, or protocol semantics changed.
3. Update `AGENTS.md`, repo skills, or other workflow docs when you discovered a reusable guardrail, gotcha, or review rule that future issue work should inherit.
4. Skip durable write-back only when the change is truly local/trivial and introduces no reusable lesson.

If you choose not to sync anything durable, make that a deliberate decision rather than an omission.

For explicit `fast` path execution:

- keep `prepare` and `finish`
- keep required-section checks, current-state checks, and validation
- skip staged-plan authoring when the work is clearly single-stage
- skip mid-task body rewrites when the issue body is already accurate enough and no material plan change occurred
- do one implementation pass: implement, validate, commit, close out
- if the issue stops looking single-stage, low-risk, or already-clear, fall back to the unchanged `full` flow immediately

## Close-out Rules

When closing the issue, leave a short completion note with:

- what was implemented
- what was intentionally not changed, if relevant
- how it was validated
- whether verifier ran, or why it was intentionally skipped
- what durable knowledge was synced back, or why none was needed
- commit or PR reference
- follow-up issue reference if work was intentionally deferred

If the work originated from an external reporter issue:

- close the internal execution issue only
- post the completion note back to the external issue as a comment
- include the internal issue link and validation summary in that comment
- do not auto-close the external issue

Do not treat close-out as ready until all applicable close gates pass:

- verifier close gate
  - medium/large issues need a durable `独立 verifier 结果：pass` record unless explicitly waived or in `workflow:fast`
- child roll-up gate
  - child issues with a parent must already have a durable roll-up recorded on the parent
- parent summary gate
  - parent issues must already expose child roll-up state, verifier state, and current close judgment in their total view
- legacy contract gate
  - resumed older issues must first be upgraded to the current workflow contract fields that the close gate depends on

The expected terminal state for a finished issue is:

- clean worktree
- no unpublished local commit left behind unless the user explicitly asked for local-only state
- `finish` has been run
- the issue is closed if its acceptance criteria are satisfied

If you cannot reach that terminal state, say exactly why and what remains blocked instead of stopping silently at a local-only midpoint.

Before finishing the turn, prefer:

```bash
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh close-plan --issue <number>
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue <number> --comment-file <file> --close
```

The first command must be green before using the second command.
`finish --close` then runs the fixed local checks, closes the issue, and releases `processing`.
