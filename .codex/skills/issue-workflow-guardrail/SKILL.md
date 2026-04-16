---
name: issue-workflow-guardrail
description: "Use when handling a GitHub issue for this repository: run the fixed prepare/lint/finish workflow first, reassess the issue against the latest code, update issue state before acting, stop when the startability state changed, execute implementable work in staged delivery, keep plans synced back to the issue, and leave a validation-focused completion note when closing the issue."
---

# Issue Workflow Guardrail

Use this skill whenever the task is centered on a GitHub issue in this repository.

Examples:

- the user gives an issue number or URL
- the user asks to complete, triage, refine, or close an issue
- the issue may be blocked, underspecified, or waiting on clarification
- the issue may have been opened by the user or by someone else

Do not run a one-time cleanup pass over old issues. Normalize an issue only when it becomes active.

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
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue <number> [--comment-file path] [--close]
```

When you intentionally want the helper output to match the chosen workflow mode, pass:

```bash
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh prepare --issue <number> --mode fast
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh lint --issue <number> --mode fast
```

`--mode full` keeps the legacy behavior.

What each command owns:

- `prepare`
  - blocks on tracked local changes before sync
  - runs `git pull --ff-only`
  - fetches the live issue snapshot from GitHub
  - claims `processing` when available
  - writes a reusable snapshot JSON under `.codex/state/issue-workflow/`
- `lint`
  - checks required issue sections
  - checks status/category/scope label shape
  - warns when the staged-plan section is still missing on a label-wise implementable issue
- `finish`
  - runs the fixed local mechanical checks
  - can post a comment, close the issue, and release `processing`

Use these commands at fixed times:

1. Before substantive issue assessment, run `prepare`.
2. After body or label edits, run `lint`.
3. Before any normal stop path for the issue, run `finish`.

Important:

- For an implementable issue, “local code is written and tests passed” is not by itself a normal stop path.
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

## Read Order

After `prepare` succeeds, read in this order:

1. the current issue body
2. current labels
3. the latest comments
4. the current code

If later comments conflict with the body, treat the latest maintainer or user comment as the current direction. Update the body if that can be done cheaply and accurately.

## Body vs Comment

Use the issue body for durable structure:

- background
- goal
- scope or non-goals
- related docs
- related files
- acceptance criteria
- staged plan (`建议范围`) when work is not truly single-stage
- implementation context (`实现参考`)
- check context (`检查参考`)
- low-priority deferred follow-ups in a dedicated `低优先级待办` section when they are too small to justify a standalone issue
- finish / knowledge-sync context (`收尾参考`)

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
7. Once the issue is implementable, fill or refresh `实现参考`, `检查参考`, and `收尾参考`.
   - `实现参考`: recommended cut, key docs/files, current preferred solution, confirmed constraints
   - `检查参考`: risky flows, regression points, exact docs/tests to re-check
   - `收尾参考`: likely knowledge write-back targets such as issue body, linked design docs, docs/general, state-machine docs, AGENTS, or repo skills
8. If later investigation or implementation changes the staged plan or any execution-context section materially, update the issue body before continuing.
9. If work uncovers a small, non-blocking, low-priority follow-up that is not worth a standalone issue, append it to `低优先级待办` in the active issue body instead of leaving it only in chat.
   - Keep entries concise and actionable.
   - Include what is deferred and why it stayed in-body instead of becoming its own issue.
   - Use this section as the canonical source for later backlog harvest.

## Reassessment Decision

After refining against the latest code, classify the issue into one of these states:

- `implementable now`
- `needs investigation`
- `needs clarification`
- `blocked`

## State-Transition Rule

Compare the reassessed state with the issue's previously recorded actionable state.

- If the state changed in either direction, update the issue body, labels, and concise evidence as needed, then run `finish --issue <number> --skip-checks` and stop there for this turn.
- If the state did not change but the issue is still not implementable, update the issue with any newly confirmed evidence, then run `finish --issue <number> --skip-checks` and stop there for this turn.
- Only when the issue was already implementable and remains implementable after reassessment may coding start immediately.

## Status Labels

If the issue cannot be started immediately, apply exactly one of:

- `status:needs-investigation`
  - use when the code or runtime path must be researched before safe implementation
- `status:needs-clarification`
  - use when product intent, user expectation, or acceptance criteria are still unclear
- `status:blocked`
  - use when an external dependency, upstream change, or awaited decision prevents progress

Remove stale status labels when the issue moves to a different blocked state or becomes implementable again.

## Blocking Comment Rules

When work cannot start, leave one concise comment that contains:

- current blocking state
- what was checked
- the exact missing question, decision, or dependency
- what reply or action would unblock the issue

Keep it short and actionable. Do not restate the full issue body.
Before you stop on this path, prefer `finish --issue <number> --comment-file <file> --skip-checks` so `processing` is released mechanically.

## Implementation Rules

If the issue was already implementable and still is after reassessment:

- do not leave a ritual “starting work” comment
- implement against the refined issue
- before each implementation stage, re-read the issue body, latest comments, current code state, and `实现参考`
- before each implementation stage, re-run any repository skills already required by the task so the next step is based on current guidance
- before validation/check work, re-read `检查参考`
- if the best next stage or any execution-context section changed materially, update the issue body first instead of leaving the new plan only in a comment
- prefer staged delivery for medium or large work
- write the current staged plan into the issue or a linked design doc before stage 1
- update the staged plan back to the issue whenever the plan or stage split changes
- when you intentionally defer a tiny follow-up that is not worth a new issue, record it under `低优先级待办` before moving on
- continue through all planned stages in the same task unless a major assumption collapsed and the remaining direction would materially diverge
- every stage must include sufficient validation, not only compilation or superficial smoke checks
- each stage should end with implementation, stage-scoped validation, and a local commit
- when the overall issue is finished, do not stop at “last stage implemented locally”; continue through publish/close-out work in the same turn unless blocked
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
- what durable knowledge was synced back, or why none was needed
- commit or PR reference
- follow-up issue reference if work was intentionally deferred

The expected terminal state for a finished issue is:

- clean worktree
- no unpublished local commit left behind unless the user explicitly asked for local-only state
- `finish` has been run
- the issue is closed if its acceptance criteria are satisfied

If you cannot reach that terminal state, say exactly why and what remains blocked instead of stopping silently at a local-only midpoint.

Before finishing the turn, prefer:

```bash
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue <number> --comment-file <file> --close
```

This runs the fixed local checks first, then closes the issue and releases `processing`.
