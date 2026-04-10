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

## Fixed Entry Points

Default to the bundled wrapper instead of redoing raw `git` / `gh` sequences by hand:

```bash
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh prepare --issue <number>
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh lint --issue <number>
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue <number> [--comment-file path] [--close]
```

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
7. If later investigation or implementation changes that staged plan materially, update the issue body before continuing.

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
- before each implementation stage, re-read the issue body, latest comments, and current code state
- before each implementation stage, re-run any repository skills already required by the task so the next step is based on current guidance
- if the best next stage changed materially, update the issue body first instead of leaving the new plan only in a comment
- prefer staged delivery for medium or large work
- write the current staged plan into the issue or a linked design doc before stage 1
- update the staged plan back to the issue whenever the plan or stage split changes
- continue through all planned stages in the same task unless a major assumption collapsed and the remaining direction would materially diverge
- every stage must include sufficient validation, not only compilation or superficial smoke checks
- each stage should end with implementation, stage-scoped validation, and a local commit
- validate the result
- update any affected design or state-machine document required by repo rules

## Close-out Rules

When closing the issue, leave a short completion note with:

- what was implemented
- what was intentionally not changed, if relevant
- how it was validated
- commit or PR reference
- follow-up issue reference if work was intentionally deferred

Before finishing the turn, prefer:

```bash
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue <number> --comment-file <file> --close
```

This runs the fixed local checks first, then closes the issue and releases `processing`.
