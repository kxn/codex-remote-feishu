package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	vscodeMigrationFlowTTL             = 30 * time.Minute
	vscodeMigrationOwnerPayloadKind    = "vscode_migrate_owner_flow"
	vscodeMigrationOwnerPayloadFlowKey = "picker_id"
	vscodeMigrationOwnerPayloadRunKey  = "option_id"
	vscodeMigrationOwnerActionRun      = "run"
)

func (a *App) nextVSCodeMigrationFlowIDLocked() string {
	a.surfaceResumeRuntime.vscodeMigrationNextSeq++
	return fmt.Sprintf("vscode-migrate-%d", a.surfaceResumeRuntime.vscodeMigrationNextSeq)
}

func (a *App) syncVSCodeMigrationFlowsLocked() {
	if a.surfaceResumeRuntime.vscodeMigrationFlows == nil {
		a.surfaceResumeRuntime.vscodeMigrationFlows = map[string]*vscodeMigrationFlowRecord{}
	}
	now := time.Now().UTC()
	for surfaceID, flow := range a.surfaceResumeRuntime.vscodeMigrationFlows {
		if strings.TrimSpace(surfaceID) == "" || flow == nil {
			delete(a.surfaceResumeRuntime.vscodeMigrationFlows, surfaceID)
			continue
		}
		if !flow.ExpiresAt.IsZero() && !flow.ExpiresAt.After(now) {
			delete(a.surfaceResumeRuntime.vscodeMigrationFlows, surfaceID)
			continue
		}
		snapshot := a.service.SurfaceSnapshot(surfaceID)
		if snapshot == nil || state.NormalizeProductMode(state.ProductMode(snapshot.ProductMode)) != state.ProductModeVSCode {
			delete(a.surfaceResumeRuntime.vscodeMigrationFlows, surfaceID)
		}
	}
}

func (a *App) activeVSCodeMigrationFlowLocked(surfaceID string) *vscodeMigrationFlowRecord {
	a.syncVSCodeMigrationFlowsLocked()
	return a.surfaceResumeRuntime.vscodeMigrationFlows[strings.TrimSpace(surfaceID)]
}

func (a *App) newVSCodeMigrationFlowLocked(surfaceID, ownerUserID, messageID, issueKey string) *vscodeMigrationFlowRecord {
	surfaceID = strings.TrimSpace(surfaceID)
	if surfaceID == "" {
		return nil
	}
	a.syncVSCodeMigrationFlowsLocked()
	now := time.Now().UTC()
	flow := &vscodeMigrationFlowRecord{
		FlowID:           a.nextVSCodeMigrationFlowIDLocked(),
		SurfaceSessionID: surfaceID,
		OwnerUserID:      strings.TrimSpace(ownerUserID),
		MessageID:        strings.TrimSpace(messageID),
		IssueKey:         strings.TrimSpace(issueKey),
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        now.Add(vscodeMigrationFlowTTL),
	}
	a.surfaceResumeRuntime.vscodeMigrationFlows[surfaceID] = flow
	return flow
}

func (a *App) refreshVSCodeMigrationFlowLocked(flow *vscodeMigrationFlowRecord, issueKey string) {
	if flow == nil {
		return
	}
	now := time.Now().UTC()
	flow.IssueKey = strings.TrimSpace(issueKey)
	flow.UpdatedAt = now
	flow.ExpiresAt = now.Add(vscodeMigrationFlowTTL)
}

func (a *App) clearVSCodeMigrationFlowLocked(surfaceID string) {
	delete(a.surfaceResumeRuntime.vscodeMigrationFlows, strings.TrimSpace(surfaceID))
}

func (a *App) recordVSCodeMigrationFlowMessageLocked(trackingKey, messageID string) {
	trackingKey = strings.TrimSpace(trackingKey)
	messageID = strings.TrimSpace(messageID)
	if trackingKey == "" || messageID == "" {
		return
	}
	a.syncVSCodeMigrationFlowsLocked()
	for _, flow := range a.surfaceResumeRuntime.vscodeMigrationFlows {
		if flow == nil || strings.TrimSpace(flow.FlowID) != trackingKey {
			continue
		}
		flow.MessageID = messageID
		a.refreshVSCodeMigrationFlowLocked(flow, flow.IssueKey)
		return
	}
}

func (a *App) requireVSCodeMigrationFlowLocked(surfaceID, flowID, actorUserID string) (*vscodeMigrationFlowRecord, []control.UIEvent) {
	surfaceID = strings.TrimSpace(surfaceID)
	flowID = strings.TrimSpace(flowID)
	flow := a.activeVSCodeMigrationFlowLocked(surfaceID)
	if flow == nil || strings.TrimSpace(flow.FlowID) != flowID {
		return nil, []control.UIEvent{vscodeMigrationStandaloneEvent(surfaceID, true, "迁移卡片已失效", []string{"这张 VS Code 迁移卡片已失效，请重新发送 `/vscode-migrate`。"}, "", "error", nil)}
	}
	actorUserID = strings.TrimSpace(firstNonEmpty(actorUserID, a.service.SurfaceActorUserID(surfaceID)))
	if owner := strings.TrimSpace(flow.OwnerUserID); owner != "" && actorUserID != "" && owner != actorUserID {
		return nil, []control.UIEvent{vscodeMigrationStandaloneEvent(surfaceID, true, "无法执行迁移", []string{"这张 VS Code 迁移卡片只允许发起者本人操作。"}, "", "error", nil)}
	}
	return flow, nil
}

func vscodeMigrationOwnerButton(label, flowID string) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label: label,
		Kind:  control.CommandCatalogButtonCallbackAction,
		CallbackValue: map[string]any{
			"kind":                             vscodeMigrationOwnerPayloadKind,
			vscodeMigrationOwnerPayloadFlowKey: strings.TrimSpace(flowID),
			vscodeMigrationOwnerPayloadRunKey:  vscodeMigrationOwnerActionRun,
		},
		Style: "primary",
	}
}

func vscodeMigrationThemeForIssue(issue *vscodeCompatibilityIssue) string {
	if issue == nil {
		return "info"
	}
	if strings.TrimSpace(issue.ButtonLabel) != "" {
		return "approval"
	}
	return "info"
}

func vscodeMigrationThemeForNotice(notice *control.Notice) string {
	if notice == nil {
		return "info"
	}
	if theme := strings.TrimSpace(notice.ThemeKey); theme != "" {
		return theme
	}
	switch strings.TrimSpace(notice.Code) {
	case "surface_resume_instance_attached", "vscode_migration_not_needed", "vscode_migration_applied":
		return "success"
	case "vscode_migration_check_failed",
		"vscode_migration_failed",
		"vscode_migration_applied_detect_failed",
		"vscode_migration_incomplete",
		"surface_resume_instance_busy",
		"surface_resume_instance_not_found":
		return "error"
	default:
		return "info"
	}
}

func vscodeMigrationButtonsForNotice(notice *control.Notice) []control.CommandCatalogButton {
	if notice == nil {
		return nil
	}
	switch strings.TrimSpace(notice.Code) {
	case "not_attached_vscode", "surface_resume_instance_busy", "surface_resume_instance_not_found":
		return []control.CommandCatalogButton{
			runCommandButton("选择实例", "/list", "primary", false),
		}
	case "surface_resume_instance_attached":
		return []control.CommandCatalogButton{
			runCommandButton("选择会话", "/use", "primary", false),
		}
	default:
		return nil
	}
}

func isVSCodeMigrationFlowNotice(notice *control.Notice) bool {
	if notice == nil {
		return false
	}
	switch strings.TrimSpace(notice.Code) {
	case "not_attached_vscode",
		"surface_resume_instance_attached",
		"surface_resume_instance_busy",
		"surface_resume_instance_not_found",
		"surface_resume_open_vscode",
		"vscode_open_required",
		"vscode_migration_check_failed",
		"vscode_migration_not_needed",
		"vscode_migration_failed",
		"vscode_migration_applied_detect_failed",
		"vscode_migration_incomplete",
		"vscode_migration_applied":
		return true
	default:
		return false
	}
}

func buildVSCodeMigrationPageView(flow *vscodeMigrationFlowRecord, inlineReplace bool, title string, summary []string, statusText, theme string, buttons []control.CommandCatalogButton) control.FeishuCommandPageView {
	view := control.FeishuCommandPageView{
		CommandID:       control.FeishuCommandVSCodeMigrate,
		Title:           strings.TrimSpace(title),
		ThemeKey:        strings.TrimSpace(theme),
		Patchable:       true,
		Breadcrumbs:     control.FeishuCommandBreadcrumbsForCommand(control.FeishuCommandVSCodeMigrate),
		SummarySections: commandCatalogSummarySections(summary...),
		StatusKind:      statusKindFromThemeKey(theme),
		StatusText:      strings.TrimSpace(statusText),
		Interactive:     len(buttons) > 0,
		DisplayStyle:    control.CommandCatalogDisplayCompactButtons,
	}
	if len(buttons) > 0 {
		view.Sections = []control.CommandCatalogSection{{
			Title: "下一步",
			Entries: []control.CommandCatalogEntry{{
				Buttons: append([]control.CommandCatalogButton(nil), buttons...),
			}},
		}}
	}
	if flow == nil || inlineReplace {
		return view
	}
	if messageID := strings.TrimSpace(flow.MessageID); messageID != "" {
		view.MessageID = messageID
		return view
	}
	view.TrackingKey = strings.TrimSpace(flow.FlowID)
	return view
}

func statusKindFromThemeKey(theme string) string {
	switch strings.TrimSpace(theme) {
	case "success":
		return "success"
	case "error":
		return "error"
	default:
		return "info"
	}
}

func vscodeMigrationPageEvent(surfaceID string, flow *vscodeMigrationFlowRecord, inlineReplace bool, title string, summary []string, statusText, theme string, buttons []control.CommandCatalogButton) control.UIEvent {
	view := buildVSCodeMigrationPageView(flow, inlineReplace, title, summary, statusText, theme, buttons)
	return control.UIEvent{
		Kind:             control.UIEventFeishuDirectCommandCatalog,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		FeishuCommandView: &control.FeishuCommandView{
			Page: &view,
		},
	}
}

func vscodeMigrationStandaloneEvent(surfaceID string, inlineReplace bool, title string, summary []string, statusText, theme string, buttons []control.CommandCatalogButton) control.UIEvent {
	return vscodeMigrationPageEvent(surfaceID, nil, inlineReplace, title, summary, statusText, theme, buttons)
}

func vscodeMigrationPromptEvent(surfaceID string, flow *vscodeMigrationFlowRecord, inlineReplace bool, issue vscodeCompatibilityIssue) control.UIEvent {
	var buttons []control.CommandCatalogButton
	if flow != nil && strings.TrimSpace(issue.ButtonLabel) != "" {
		buttons = []control.CommandCatalogButton{vscodeMigrationOwnerButton(issue.ButtonLabel, flow.FlowID)}
	}
	return vscodeMigrationPageEvent(
		surfaceID,
		flow,
		inlineReplace,
		issue.Title,
		[]string{strings.TrimSpace(issue.Summary)},
		issue.ActionText,
		vscodeMigrationThemeForIssue(&issue),
		buttons,
	)
}

func vscodeMigrationNoticeEvent(surfaceID string, flow *vscodeMigrationFlowRecord, inlineReplace bool, notice *control.Notice) control.UIEvent {
	if notice == nil {
		return vscodeMigrationStandaloneEvent(surfaceID, inlineReplace, "VS Code 迁移", nil, "", "info", nil)
	}
	return vscodeMigrationPageEvent(
		surfaceID,
		flow,
		inlineReplace,
		firstNonEmpty(strings.TrimSpace(notice.Title), "VS Code 迁移"),
		[]string{strings.TrimSpace(notice.Text)},
		"",
		vscodeMigrationThemeForNotice(notice),
		vscodeMigrationButtonsForNotice(notice),
	)
}

func (a *App) routeVSCodeMigrationFlowNoticeLocked(event control.UIEvent) control.UIEvent {
	if !isVSCodeMigrationFlowNotice(event.Notice) {
		return event
	}
	flow := a.activeVSCodeMigrationFlowLocked(event.SurfaceSessionID)
	if flow == nil {
		flow = a.newVSCodeMigrationFlowLocked(
			event.SurfaceSessionID,
			a.service.SurfaceActorUserID(event.SurfaceSessionID),
			"",
			"",
		)
		if flow == nil {
			return event
		}
	}
	a.refreshVSCodeMigrationFlowLocked(flow, "")
	updated := vscodeMigrationNoticeEvent(event.SurfaceSessionID, flow, false, event.Notice)
	updated.GatewayID = firstNonEmpty(strings.TrimSpace(event.GatewayID), strings.TrimSpace(updated.GatewayID))
	updated.SourceMessageID = strings.TrimSpace(event.SourceMessageID)
	updated.SourceMessagePreview = strings.TrimSpace(event.SourceMessagePreview)
	return updated
}
