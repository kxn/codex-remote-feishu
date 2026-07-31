package daemon

import (
	"strings"
	"time"
)

func surfaceResumeFailureSpecificity(code string) int {
	switch strings.TrimSpace(code) {
	case "headless_restore_provider_unavailable",
		"headless_restore_claude_profile_unavailable",
		"headless_restore_workspace_missing",
		"profile_definition_incomplete",
		"profile_secret_missing",
		"oauth_missing",
		"oauth_probe_unknown",
		"oauth_deployment_unsupported",
		"codex_capability_unsupported",
		"profile_revision_unavailable":
		return 3
	case "headless_restore_runtime_unavailable":
		return 2
	case "headless_restore_start_failed",
		"headless_restore_start_timeout":
		return 1
	default:
		return 0
	}
}

func shouldUpgradeSurfaceResumeStickyFailure(current, next string) bool {
	return surfaceResumeFailureSpecificity(next) > surfaceResumeFailureSpecificity(current)
}

func isTerminalSurfaceResumeFailure(code string) bool {
	switch strings.TrimSpace(code) {
	case "headless_restore_workspace_missing",
		"headless_restore_thread_not_found",
		"headless_restore_thread_cwd_missing",
		"headless_restore_provider_unavailable",
		"headless_restore_claude_profile_unavailable",
		"headless_restore_runtime_unavailable",
		"thread_not_found",
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
		"profile_revision_unavailable":
		return true
	default:
		return false
	}
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
	displayCode := strings.TrimSpace(firstNonEmpty(recovery.StickyFailureCode, code))
	if displayCode == "" {
		return "", false
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
