---
name: issue-doc-sync
description: "Use when syncing closed GitHub issues back into repo docs. Scans closed issues incrementally by updatedAt, reuses a tracked state cache, surfaces only candidates that changed since the last extraction, and records skip/merge/new-doc decisions for this repository."
---

# issue-doc-sync

Use this skill when the user asks to:

- sync closed GitHub issues back into `docs/`
- extract durable design knowledge from closed issues
- avoid re-reading unchanged closed issues
- maintain the tracked issue-to-doc sync state in this repository

## Workflow

1. Sync the current branch first.
   - Run `git pull --ff-only`.
   - Do not assess issues against stale local code or stale tracked cache.
2. List only changed closed issues.
   - Prefer `scripts/issue-doc-sync/review.sh plan`.
   - This compares GitHub `updatedAt` against `.codex/state/issue-doc-sync/state.json`.
   - The default processing order is old to new by `closedAt`, with issue number fallback for same-time ties.
3. Review each candidate issue.
   - Prefer `scripts/issue-doc-sync/review.sh inspect [issue-number]`.
   - If no issue number is given, the runner opens the oldest pending candidate automatically.
   - If current docs already cover the durable result, skip it and record why.
   - If an existing canonical doc is the right home, merge into that doc.
   - If no suitable doc exists, create a new doc under the correct lifecycle directory.
4. Update docs.
   - Every `docs/**/*.md` file must keep the visible metadata block under the title:
     - `Type`
     - `Updated`
     - `Summary`
   - If you add or move a lifecycle doc, update `docs/README.md` in the same change.
5. Record the decision in the tracked state cache.
   - Prefer `scripts/issue-doc-sync/review.sh record [issue-number] --decision ... --reason ...`.
   - If no issue number is given, the runner records against the oldest pending candidate.
   - Required fields:
     - `--issue`
     - `--decision skip|merge|new-doc`
     - `--reason`
   - Add `--target-doc` once per touched doc path when the decision is `merge` or `new-doc`.
   - The underlying `record` command now auto-fills issue metadata from GitHub when not provided.
   - If a target doc was already touched by a newer synced issue, `record` refuses by default and requires `--force` for an intentional backfill.
6. Validate.
   - The runner re-runs `plan` automatically after `record`.
   - You can also run `scripts/issue-doc-sync/review.sh plan` and confirm unchanged issues disappear from the candidate set.

## Decision Rules

- Default doc target:
  - `docs/implemented/` for feature-level implemented behavior
  - `docs/general/` only when the conclusion is a longer-lived repo baseline or canonical process
- Skip when:
  - the durable conclusion is already covered by current docs
  - the issue is mostly process chatter, copy tweaks, or low-value operational history
- Prefer merge over new doc when one existing canonical doc clearly owns the topic.

## State Cache

- Cache path: `.codex/state/issue-doc-sync/state.json`
- The cache is tracked in git on purpose.
- Each decision should be committed together with the matching doc change.
- Expected tracked state fields:
  - issue number
  - GitHub `updatedAt`
  - decision
  - reason
  - target doc paths
  - source issue URL

## References

- For the doc metadata template and decision examples, read [references/doc-template.md](./references/doc-template.md).
