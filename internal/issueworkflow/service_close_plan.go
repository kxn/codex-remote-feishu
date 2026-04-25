package issueworkflow

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) ClosePlan(ctx context.Context, opts ClosePlanOptions) (ClosePlanResult, error) {
	result := ClosePlanResult{
		IssueNumber:  opts.IssueNumber,
		WorkflowMode: normalizeWorkflowMode(opts.WorkflowMode),
	}
	if opts.IssueNumber <= 0 {
		return result, fmt.Errorf("close-plan requires a positive issue number")
	}
	repo, err := s.resolveRepo(ctx, opts.Repo)
	if err != nil {
		return result, err
	}
	result.Repo = repo.String()
	issue, err := s.GitHub.FetchIssue(ctx, repo, opts.IssueNumber, closeGateCommentsLimit)
	if err != nil {
		return result, err
	}
	result.Issue = &issue
	result.Lint = BuildLintReport(issue, result.WorkflowMode)
	if workflowCheck := workflowContractCheck(result.Lint); workflowCheck != nil {
		result.Checks = append(result.Checks, *workflowCheck)
	}
	closeChecks, err := s.closeGateChecks(ctx, repo, issue, result.WorkflowMode)
	if err != nil {
		return result, err
	}
	result.Checks = append(result.Checks, closeChecks...)
	result.CloseReady = !hasFailedCheck(result.Checks)
	structure := analyzeIssueStructure(issue.Number, issue.Body, scanDocumentSections(issue.Body))
	result.NextActions = buildClosePlanActions(issue, result.Lint, result.Checks, structure, result.WorkflowMode, result.CloseReady)
	return result, nil
}

func buildClosePlanActions(issue Issue, report LintReport, checks []CheckResult, structure issueStructure, mode WorkflowMode, closeReady bool) []ClosePlanAction {
	actions := make([]ClosePlanAction, 0, 6)
	seen := map[string]struct{}{}
	add := func(action ClosePlanAction) {
		if action.Code == "" {
			return
		}
		if _, ok := seen[action.Code]; ok {
			return
		}
		seen[action.Code] = struct{}{}
		actions = append(actions, action)
	}
	if closeReady {
		add(ClosePlanAction{
			Code:    "finish_close",
			Summary: "issue-side close gates are ready; run `finish --close` once local validation, commit, and publish are ready",
			Command: finishCloseCommand(issue.Number, mode),
		})
		return actions
	}
	enforcePlanningContract := report.CurrentRecordedState == statusLabelNeedsPlan || report.CurrentRecordedState == statusLabelImplementable
	if enforcePlanningContract || report.WorkflowGuardrails.ExecutionDecisionRequired {
		problems := make([]string, 0, 5)
		if len(report.RequiredMissing) > 0 {
			problems = append(problems, "add required sections: "+strings.Join(report.RequiredMissing, ", "))
		}
		if containsSection(report.PreferredMissing, "建议范围") {
			problems = append(problems, "add `建议范围`")
		}
		if report.CurrentRecordedState == statusLabelImplementable {
			if missingExecutionSections := intersectSections(report.PreferredMissing, executionSections); len(missingExecutionSections) > 0 {
				problems = append(problems, "fill execution context sections: "+strings.Join(missingExecutionSections, ", "))
			}
		}
		if !report.WorkflowGuardrails.ExecutionDecisionSectionFound && report.WorkflowGuardrails.ExecutionDecisionRequired {
			problems = append(problems, "add `执行决策`")
		}
		if len(report.WorkflowGuardrails.ExecutionDecisionMissingItems) > 0 {
			problems = append(problems, "fill execution decision items: "+strings.Join(report.WorkflowGuardrails.ExecutionDecisionMissingItems, ", "))
		}
		if report.WorkflowGuardrails.SnapshotRequired && len(report.WorkflowGuardrails.SnapshotMissingFields) > 0 {
			problems = append(problems, "fill execution snapshot fields: "+strings.Join(report.WorkflowGuardrails.SnapshotMissingFields, ", "))
		}
		if len(report.WorkflowGuardrails.SnapshotContradictions) > 0 {
			problems = append(problems, "resolve execution snapshot contradictions: "+strings.Join(report.WorkflowGuardrails.SnapshotContradictions, "; "))
		}
		if len(problems) > 0 {
			add(ClosePlanAction{
				Code:          "refresh_issue_workflow_contract",
				BlockingCheck: "issue_workflow_contract",
				Summary:       "refresh the issue workflow contract before close-out: " + strings.Join(problems, "; "),
			})
		}
	}
	if check := findNamedCheck(checks, "issue_close_verifier_gate"); check != nil && check.Status == CheckStatusFail {
		code := "record_verifier_result"
		summary := "run the independent verifier and durably record `独立 verifier 结果：pass` before close-out"
		if result, ok := lastVerifierResult(issue); ok && result != "pass" {
			code = "resolve_verifier_failures"
			summary = "latest verifier result is `" + result + "`; fix the remaining gaps and record a new passing verifier result before close-out"
		}
		add(ClosePlanAction{
			Code:          code,
			BlockingCheck: check.Name,
			Summary:       summary,
		})
	}
	if structure.LegacyChildParentRef {
		add(ClosePlanAction{
			Code:          "promote_parent_link_section",
			BlockingCheck: "issue_close_child_contract_gate",
			Summary:       fmt.Sprintf("promote the parent link into a dedicated `父 issue` section that references `#%d`", structure.ParentIssueNumber),
		})
	}
	if structure.ParentIssueNumber > 0 {
		if check := findNamedCheck(checks, "issue_close_rollup_gate"); check != nil && check.Status == CheckStatusFail {
			add(ClosePlanAction{
				Code:          "update_parent_rollup",
				BlockingCheck: check.Name,
				Summary:       fmt.Sprintf("write a durable child roll-up for `#%d` into parent issue `#%d` before close-out", issue.Number, structure.ParentIssueNumber),
			})
		}
	}
	if structure.IsParent {
		if check := findNamedCheck(checks, "issue_close_parent_summary_gate"); check != nil && check.Status == CheckStatusFail {
			problems := make([]string, 0, 3)
			if len(structure.MissingParentSections) > 0 {
				problems = append(problems, "add sections: "+strings.Join(structure.MissingParentSections, ", "))
			}
			if len(structure.MissingParentSummaryCols) > 0 {
				problems = append(problems, "add summary columns: "+strings.Join(structure.MissingParentSummaryCols, ", "))
			}
			if missingRollups := missingChildRollups(issue, structure.ChildIssueNumbers); len(missingRollups) > 0 {
				problems = append(problems, "record child roll-ups for: "+strings.Join(missingRollups, ", "))
			}
			summary := "repair the parent close-out summary before close-out"
			if len(problems) > 0 {
				summary += ": " + strings.Join(problems, "; ")
			}
			add(ClosePlanAction{
				Code:          "repair_parent_closeout_summary",
				BlockingCheck: check.Name,
				Summary:       summary,
			})
		}
	}
	add(ClosePlanAction{
		Code:    "rerun_close_plan",
		Summary: "rerun the close readiness check after the blocking issue-side state is updated",
		Command: closePlanCommand(issue.Number, mode),
	})
	return actions
}

func closePlanCommand(issueNumber int, mode WorkflowMode) string {
	cmd := fmt.Sprintf("bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh close-plan --issue %d", issueNumber)
	if normalizeWorkflowMode(mode) == WorkflowModeFast {
		cmd += " --mode fast"
	}
	return cmd
}

func finishCloseCommand(issueNumber int, mode WorkflowMode) string {
	cmd := fmt.Sprintf("bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue %d --close", issueNumber)
	if normalizeWorkflowMode(mode) == WorkflowModeFast {
		cmd += " --mode fast"
	}
	return cmd
}

func findNamedCheck(checks []CheckResult, name string) *CheckResult {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}
