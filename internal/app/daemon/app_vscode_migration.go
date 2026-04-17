package daemon

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const vscodeMigrateCommandText = "/vscode-migrate"
const vscodeCompatibilityRetryBackoff = 30 * time.Second

type vscodeCompatibilityIssue struct {
	Key         string
	Title       string
	Summary     string
	ActionText  string
	ButtonLabel string
	SuccessText string
}

type vscodeCompatibilityPromptTarget struct {
	SurfaceSessionID string
	GatewayID        string
}

func classifyVSCodeCompatibilityIssue(detect vscodeDetectResponse) *vscodeCompatibilityIssue {
	hasTarget := vscodeHasMigrationTarget(detect)
	legacySettings := strings.EqualFold(strings.TrimSpace(detect.CurrentMode), "editor_settings") || detect.Settings.MatchesBinary
	if legacySettings {
		issue := &vscodeCompatibilityIssue{
			Key:         "legacy_editor_settings",
			Title:       "VS Code 接入需要迁移",
			Summary:     "检测到这台机器仍在使用旧版 settings.json 覆盖。它会把 host 侧 override 带进 Remote SSH，会继续干扰远端 VS Code 会话。新版本已经统一收敛到扩展入口 managed shim。",
			SuccessText: "已迁移到扩展入口 managed shim。请重新打开 VS Code 开始使用。",
		}
		if hasTarget {
			issue.ActionText = "确认这台机器上的 VS Code 已关闭后，再点击下方按钮执行迁移。完成后请重新打开 VS Code。"
			issue.ButtonLabel = "迁移并重新接入"
		} else {
			issue.ActionText = "当前还没检测到可接管的 VS Code 扩展入口。请先在这台机器上打开一次 VS Code，并确保 Codex 扩展已经安装，然后再回来迁移。"
		}
		return issue
	}
	if detect.NeedsShimReinstall {
		issue := &vscodeCompatibilityIssue{
			Key:         "managed_shim_reinstall",
			Title:       "VS Code 接入需要修复",
			Summary:     "检测到当前 managed shim 已失效，常见原因是 VS Code 扩展升级后入口发生了变化。需要重新接管最新扩展入口后，vscode mode 才能继续稳定使用。",
			SuccessText: "已重新接管最新 VS Code 扩展入口。请重新打开 VS Code 开始使用。",
		}
		if hasTarget {
			issue.ActionText = "确认这台机器上的 VS Code 已关闭后，再点击下方按钮重新接入最新扩展入口。完成后请重新打开 VS Code。"
			issue.ButtonLabel = "重新接入扩展入口"
		} else {
			issue.ActionText = "当前还没检测到可接管的 VS Code 扩展入口。请先在这台机器上打开一次 VS Code，并确保 Codex 扩展已经安装，然后再回来修复。"
		}
		return issue
	}
	return nil
}

func vscodeHasMigrationTarget(detect vscodeDetectResponse) bool {
	return strings.TrimSpace(detect.LatestBundleEntrypoint) != "" || strings.TrimSpace(detect.RecordedBundleEntrypoint) != ""
}

func vscodeMigrationCatalog(issue vscodeCompatibilityIssue) control.FeishuDirectCommandCatalog {
	entry := control.CommandCatalogEntry{
		Description: strings.TrimSpace(issue.ActionText),
	}
	interactive := strings.TrimSpace(issue.ButtonLabel) != ""
	if interactive {
		entry.Buttons = []control.CommandCatalogButton{{
			Label:       issue.ButtonLabel,
			CommandText: vscodeMigrateCommandText,
		}}
	}
	return control.FeishuDirectCommandCatalog{
		Title:       issue.Title,
		Summary:     issue.Summary,
		Interactive: interactive,
		Sections: []control.CommandCatalogSection{{
			Entries: []control.CommandCatalogEntry{entry},
		}},
	}
}

func (a *App) currentVSCodeCompatibilityIssue() (*vscodeCompatibilityIssue, error) {
	detect, err := a.detectVSCodeCompatibility()
	if err != nil {
		return nil, err
	}
	return classifyVSCodeCompatibilityIssue(detect), nil
}

func (a *App) maybePromptVSCodeCompatibilityLocked(surfaceFilter string) ([]control.UIEvent, bool) {
	return a.maybePromptVSCodeCompatibilityAtLocked(surfaceFilter, time.Now().UTC())
}

func (a *App) maybePromptVSCodeCompatibilityAtLocked(surfaceFilter string, now time.Time) ([]control.UIEvent, bool) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	a.syncVSCodeMigrationPromptStateLocked()
	targets := a.detachedVSCodeCompatibilityTargetsLocked(surfaceFilter)
	if len(targets) == 0 {
		if surfaceFilter != "" {
			delete(a.surfaceResumeRuntime.vscodeMigrationPrompts, strings.TrimSpace(surfaceFilter))
		}
		return nil, false
	}
	issue, pending := a.cachedVSCodeCompatibilityIssueLocked(now)
	if pending {
		return nil, true
	}
	if issue == nil {
		a.clearVSCodeMigrationPromptsLocked()
		return nil, false
	}

	events := []control.UIEvent{}
	for _, target := range targets {
		if a.surfaceResumeRuntime.vscodeMigrationPrompts[target.SurfaceSessionID] == issue.Key {
			continue
		}
		a.surfaceResumeRuntime.vscodeMigrationPrompts[target.SurfaceSessionID] = issue.Key
		catalog := vscodeMigrationCatalog(*issue)
		events = append(events, control.UIEvent{
			Kind:                       control.UIEventFeishuDirectCommandCatalog,
			GatewayID:                  target.GatewayID,
			SurfaceSessionID:           target.SurfaceSessionID,
			FeishuDirectCommandCatalog: &catalog,
		})
	}
	return events, true
}

func (a *App) syncVSCodeMigrationPromptStateLocked() {
	if a.surfaceResumeRuntime.vscodeMigrationPrompts == nil {
		a.surfaceResumeRuntime.vscodeMigrationPrompts = map[string]string{}
	}
	for surfaceID := range a.surfaceResumeRuntime.vscodeMigrationPrompts {
		snapshot := a.service.SurfaceSnapshot(surfaceID)
		if snapshot == nil || state.NormalizeProductMode(state.ProductMode(snapshot.ProductMode)) != state.ProductModeVSCode {
			delete(a.surfaceResumeRuntime.vscodeMigrationPrompts, surfaceID)
		}
	}
}

func (a *App) clearVSCodeMigrationPromptsLocked() {
	for surfaceID := range a.surfaceResumeRuntime.vscodeMigrationPrompts {
		delete(a.surfaceResumeRuntime.vscodeMigrationPrompts, surfaceID)
	}
}

func (a *App) detectVSCodeCompatibility() (vscodeDetectResponse, error) {
	if a.vscodeDetect != nil {
		return a.vscodeDetect()
	}
	return a.buildVSCodeDetectResponse()
}

func (a *App) cachedVSCodeCompatibilityIssueLocked(now time.Time) (*vscodeCompatibilityIssue, bool) {
	if a.vscodeCompatibility.Checked {
		return a.vscodeCompatibility.Issue, false
	}
	a.maybeStartVSCodeCompatibilityRefreshLocked(now)
	return nil, a.vscodeCompatibility.RefreshInFlight
}

func (a *App) maybeStartVSCodeCompatibilityRefreshLocked(now time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if a.vscodeCompatibility.Checked || a.vscodeCompatibility.RefreshInFlight {
		return
	}
	if !a.vscodeCompatibility.NextRetryAt.IsZero() && now.Before(a.vscodeCompatibility.NextRetryAt) {
		return
	}
	token := a.vscodeCompatibility.RefreshToken
	a.vscodeCompatibility.RefreshInFlight = true
	go a.refreshVSCodeCompatibilityAsync(token, now)
}

func (a *App) refreshVSCodeCompatibilityAsync(token uint64, startedAt time.Time) {
	issue, err := a.currentVSCodeCompatibilityIssue()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finishVSCodeCompatibilityRefreshLocked(token, startedAt, issue, err)
}

func (a *App) finishVSCodeCompatibilityRefreshLocked(token uint64, startedAt time.Time, issue *vscodeCompatibilityIssue, err error) {
	if token != a.vscodeCompatibility.RefreshToken {
		return
	}
	a.vscodeCompatibility.RefreshInFlight = false
	if err != nil {
		log.Printf("detect vscode compatibility issue failed: err=%v", err)
		a.vscodeCompatibility.Checked = false
		a.vscodeCompatibility.Issue = nil
		if startedAt.IsZero() {
			startedAt = time.Now().UTC()
		}
		a.vscodeCompatibility.NextRetryAt = startedAt.Add(vscodeCompatibilityRetryBackoff)
		return
	}
	a.vscodeCompatibility.Checked = true
	a.vscodeCompatibility.Issue = issue
	a.vscodeCompatibility.NextRetryAt = time.Time{}
	if a.shuttingDown {
		return
	}
	now := time.Now().UTC()
	promptEvents, blocked := a.maybePromptVSCodeCompatibilityAtLocked("", now)
	a.handleUIEventsLocked(context.Background(), promptEvents)
	if blocked {
		return
	}
	vscodeRecoveryEvents := a.maybeRecoverVSCodeSurfacesLocked(now)
	vscodeRecoveryEvents = append(vscodeRecoveryEvents, a.maybePromptDetachedVSCodeSurfacesLocked()...)
	a.handleUIEventsLocked(context.Background(), vscodeRecoveryEvents)
}

func (a *App) invalidateVSCodeCompatibilityCacheLocked() {
	a.vscodeCompatibility.Checked = false
	a.vscodeCompatibility.Issue = nil
	a.vscodeCompatibility.RefreshInFlight = false
	a.vscodeCompatibility.NextRetryAt = time.Time{}
	a.vscodeCompatibility.RefreshToken++
}

func (a *App) detachedVSCodeCompatibilityTargetsLocked(surfaceFilter string) []vscodeCompatibilityPromptTarget {
	surfaceFilter = strings.TrimSpace(surfaceFilter)
	targets := []vscodeCompatibilityPromptTarget{}
	for _, surface := range a.service.Surfaces() {
		if surface == nil {
			continue
		}
		surfaceID := strings.TrimSpace(surface.SurfaceSessionID)
		if surfaceFilter != "" && surfaceID != surfaceFilter {
			continue
		}
		if state.NormalizeProductMode(surface.ProductMode) != state.ProductModeVSCode {
			continue
		}
		if strings.TrimSpace(surface.AttachedInstanceID) != "" || surface.PendingHeadless != nil {
			continue
		}
		targets = append(targets, vscodeCompatibilityPromptTarget{
			SurfaceSessionID: surfaceID,
			GatewayID:        strings.TrimSpace(surface.GatewayID),
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].SurfaceSessionID < targets[j].SurfaceSessionID
	})
	return targets
}

func (a *App) handleVSCodeMigrateCommand(command control.DaemonCommand) []control.UIEvent {
	a.mu.Unlock()
	detect, err := a.buildVSCodeDetectResponse()
	a.mu.Lock()
	if err != nil {
		return vscodeMigrationNotice(command.SurfaceSessionID, "vscode_migration_check_failed", "VS Code 迁移检查失败", fmt.Sprintf("无法检查当前 VS Code 接入状态：%v", err))
	}
	issue := classifyVSCodeCompatibilityIssue(detect)
	if issue == nil {
		return vscodeMigrationNotice(command.SurfaceSessionID, "vscode_migration_not_needed", "无需迁移", "当前 VS Code 接入已经是最新状态，无需再次迁移。")
	}
	a.mu.Unlock()
	err = a.applyVSCodeIntegration(vscodeApplyRequest{Mode: "managed_shim"})
	a.mu.Lock()
	if err != nil {
		log.Printf("apply vscode migration failed: surface=%s err=%v", command.SurfaceSessionID, err)
		return vscodeMigrationNotice(command.SurfaceSessionID, "vscode_migration_failed", "迁移失败", fmt.Sprintf("迁移扩展入口失败：%v。请确认 VS Code 已关闭，并且这台机器上的 Codex 扩展已经安装后再重试。", err))
	}
	a.invalidateVSCodeCompatibilityCacheLocked()

	a.mu.Unlock()
	remaining, err := a.currentVSCodeCompatibilityIssue()
	a.mu.Lock()
	if err != nil {
		return vscodeMigrationNotice(command.SurfaceSessionID, "vscode_migration_applied_detect_failed", "迁移已执行", fmt.Sprintf("扩展入口已经更新，但后续状态检查失败：%v。请重新打开 VS Code 后再试。", err))
	}
	if remaining != nil {
		return vscodeMigrationNotice(command.SurfaceSessionID, "vscode_migration_incomplete", "迁移未完成", remaining.Summary)
	}

	delete(a.surfaceResumeRuntime.vscodeMigrationPrompts, strings.TrimSpace(command.SurfaceSessionID))
	a.surfaceResumeRuntime.vscodeResumeNotices[strings.TrimSpace(command.SurfaceSessionID)] = true
	return vscodeMigrationNotice(command.SurfaceSessionID, "vscode_migration_applied", issue.Title, issue.SuccessText)
}

func vscodeMigrationNotice(surfaceID, code, title, text string) []control.UIEvent {
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		Notice: &control.Notice{
			Code:  code,
			Title: strings.TrimSpace(title),
			Text:  strings.TrimSpace(text),
		},
	}}
}
