package daemon

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func surfaceResumeFailureSpecificity(code string) int {
	switch strings.TrimSpace(code) {
	case "headless_restore_profile_unavailable",
		"headless_restore_claude_profile_unavailable",
		"headless_restore_workspace_missing",
		"profile_definition_incomplete",
		"profile_secret_missing",
		"oauth_missing",
		"oauth_probe_unknown",
		"oauth_deployment_unsupported",
		"codex_capability_unsupported",
		"codex_probe_contract_mismatch",
		"managed_model_catalog_missing",
		"profile_revision_unavailable":
		return 3
	case "headless_restore_runtime_unavailable":
		return 2
	case "codex_binary_unavailable",
		"codex_probe_timeout",
		"codex_probe_unavailable":
		return 2
	case "headless_restore_start_failed",
		"headless_restore_start_timeout":
		return 1
	default:
		return 0
	}
}

func shouldUpgradeSurfaceResumeStickyFailure(current, next string) bool {
	if isTerminalSurfaceResumeFailure(next) && !isTerminalSurfaceResumeFailure(current) {
		return true
	}
	return surfaceResumeFailureSpecificity(next) > surfaceResumeFailureSpecificity(current)
}

func isTerminalSurfaceResumeFailure(code string) bool {
	switch strings.TrimSpace(code) {
	case "headless_restore_workspace_missing",
		"headless_restore_thread_cwd_missing",
		"headless_restore_profile_unavailable",
		"headless_restore_claude_profile_unavailable",
		"headless_restore_runtime_unavailable",
		"thread_cwd_missing",
		"workspace_not_found",
		"surface_resume_target_not_found",
		"surface_resume_instance_not_found",
		"profile_definition_incomplete",
		"profile_secret_missing",
		"oauth_missing",
		"oauth_probe_unknown",
		"oauth_deployment_unsupported",
		"codex_capability_unsupported",
		"codex_probe_contract_mismatch",
		"managed_model_catalog_missing",
		"profile_revision_unavailable":
		return true
	default:
		return false
	}
}

func shouldEmitSurfaceResumeFailureNotice(recovery *surfaceResumeRecoveryState, code string) bool {
	if recovery != nil && strings.EqualFold(strings.TrimSpace(recovery.Entry.ProductMode), "vscode") {
		return true
	}
	return isTerminalSurfaceResumeFailure(code)
}

func (a *App) recordSurfaceResumeFailureLocked(surfaceID, code string, now time.Time) (string, bool) {
	recovery := a.surfaceResumeRuntime.recovery[strings.TrimSpace(surfaceID)]
	if recovery == nil {
		return strings.TrimSpace(code), false
	}
	code = strings.TrimSpace(code)
	recovery.LastAttemptAt = now
	recovery.NextAttemptAt = now.Add(surfaceResumeRetryBackoff)
	recovery.LastFailureCode = code
	if isTerminalSurfaceResumeFailure(code) {
		recovery.TerminalFailureCode = code
	}
	if shouldUpgradeSurfaceResumeStickyFailure(recovery.StickyFailureCode, code) {
		recovery.StickyFailureCode = code
	}
	displayCode := strings.TrimSpace(xutil.FirstNonEmpty(recovery.StickyFailureCode, code))
	if displayCode == "" {
		return "", false
	}
	if !shouldEmitSurfaceResumeFailureNotice(recovery, displayCode) {
		return displayCode, false
	}
	if recovery.LastNoticeCode == "" {
		recovery.LastNoticeCode = displayCode
		return displayCode, true
	}
	if displayCode == recovery.LastNoticeCode {
		return displayCode, false
	}
	if recovery.StickyFailureCode != "" {
		recovery.LastNoticeCode = displayCode
		return displayCode, true
	}
	return displayCode, false
}
