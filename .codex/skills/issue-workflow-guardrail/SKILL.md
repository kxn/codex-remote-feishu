---
name: issue-workflow-guardrail
description: "Use when handling a GitHub issue for this repository: triage whether it is implementable, read the latest comments before acting, refine the issue body with confirmed facts, decide whether to implement now or block it, apply the correct status label when work cannot start, keep pre-implementation comments limited to useful live discussion, and leave a validation-focused completion note when closing the issue."
---

# Issue Workflow Guardrail

Use this skill whenever the task is centered on a GitHub issue in this repository.

Examples:

- the user gives an issue number or URL
- the user asks to complete, triage, refine, or close an issue
- the issue may be blocked, underspecified, or waiting on clarification

## Read Order

Before making any issue decision, read in this order:

1. the current issue body
2. current labels
3. the latest comments

If later comments conflict with the body, treat the latest maintainer or user comment as the current direction. Update the body if that can be done cheaply and accurately.

## First Decision

Classify the issue into one of these states:

- implementable now
- needs investigation
- needs clarification
- blocked

Do not start coding until the issue is clearly in `implementable now`.

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

When picking up an issue:

1. Check whether `背景`, `目标`, and `完成标准` are present and specific enough.
2. If related docs or files can be identified cheaply from repo context, add them.
3. If scope or non-goals are already clear, add them.
4. If original history or motivation cannot be reconstructed, do not invent it.
   - Record only the current confirmed background.
   - Mark missing original context as `待补充` when needed.
5. If the issue is still too broad, narrow it or split follow-up issues before implementation.
6. If staged implementation is expected, write the current staged plan into the issue body before coding.
7. If later investigation or implementation changes that staged plan materially, update the issue body before continuing.

## Status Labels

If the issue cannot be started immediately, apply exactly one of:

- `status:needs-investigation`
  - use when the code or runtime path must be researched before safe implementation
- `status:needs-clarification`
  - use when product intent, user expectation, or acceptance criteria are still unclear
- `status:blocked`
  - use when an external dependency, upstream change, or awaited decision prevents progress

Remove stale status labels when the issue moves to a new state.

## Blocking Comment Rules

When work cannot start, leave one concise comment that contains:

- current blocking state
- what was checked
- the exact missing question, decision, or dependency
- what reply or action would unblock the issue

Keep it short and actionable. Do not restate the full issue body.

## Implementation Rules

If the issue is implementable now:

- do not leave a ritual “starting work” comment
- implement against the refined issue
- before each implementation stage, re-read the issue body, latest comments, and current code state
- before each implementation stage, re-run any repository skills already required by the task so the next step is based on current guidance
- if the best next stage changed materially, update the issue body first instead of leaving the new plan only in a comment
- validate the result
- update any affected design or state-machine document required by repo rules

## Close-out Rules

When closing the issue, leave a short completion note with:

- what was implemented
- what was intentionally not changed, if relevant
- how it was validated
- commit or PR reference
- follow-up issue reference if work was intentionally deferred
