package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamUpgradeCandidate struct {
	Mode             upgradeCommandMode    `json:"mode"`
	StateFingerprint string                `json:"state_fingerprint"`
	TargetVersion    string                `json:"target_version"`
	Source           install.UpgradeSource `json:"source"`
	LocalArtifact    string                `json:"local_artifact,omitempty"`
	LocalHash        string                `json:"local_hash,omitempty"`
}

func teamUpgradeFingerprint(state install.InstallState) string {
	raw, _ := json.Marshal([]any{state.CurrentVersion, state.CurrentBinaryPath, state.CurrentTrack, state.PendingUpgrade})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (a *App) teamUpgradeAction(ctx context.Context, source teamHumanSource, parsed parsedUpgradeCommand, state install.InstallState) (string, error) {
	if parsed.Mode == upgradeCommandSetTrack {
		return a.teamHostAction(ctx, source, "host.upgrade.track", struct {
			Track  install.ReleaseTrack
			Source workstore.PrivateActionSource
		}{parsed.Track, source.Authority}, func() (string, error) {
			if !install.CurrentBuildAllowsReleaseTrack(parsed.Track) {
				return "", fmt.Errorf("%s", unsupportedTrackMessage(parsed.Track))
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			fresh, _, e := a.loadUpgradeStateLocked(true)
			if e != nil {
				return "", e
			}
			if a.upgradeRuntime.CheckInFlight || a.upgradeRuntime.StartInFlight || pendingUpgradeBusy(fresh.PendingUpgrade) {
				return "", fmt.Errorf("已有升级事务正在进行")
			}
			fresh.CurrentTrack = parsed.Track
			fresh.LastKnownLatestVersion = ""
			if clearPendingCandidateOnTrackSwitch(fresh.PendingUpgrade, parsed.Track) {
				fresh.PendingUpgrade = nil
			}
			if e = a.writeUpgradeStateLocked(fresh); e != nil {
				return "", e
			}
			return "发布轨道已实际保存为：" + string(parsed.Track), nil
		})
	}
	if parsed.Mode == upgradeCommandCodex {
		return a.queueTeamCodexUpgrade(ctx, source)
	}
	candidate := teamUpgradeCandidate{Mode: parsed.Mode, StateFingerprint: teamUpgradeFingerprint(state)}
	switch parsed.Mode {
	case upgradeCommandLatest:
		track := state.CurrentTrack
		if track == "" {
			track = defaultUpgradeTrackForState(state)
		}
		lookup := a.upgradeRuntime.Lookup
		if lookup == nil {
			lookup = a.defaultReleaseLookup
		}
		release, e := lookup(ctx, track)
		if e != nil {
			return "", e
		}
		candidate.TargetVersion = release.TagName
		candidate.Source = install.UpgradeSourceRelease
	case upgradeCommandDev:
		if !install.CurrentBuildAllowsDevUpgrade() {
			return "", fmt.Errorf("当前构建不支持 dev 来源升级")
		}
		lookup := a.upgradeRuntime.DevManifest
		if lookup == nil {
			lookup = a.defaultDevManifestLookup
		}
		manifest, _, e := lookup(ctx)
		if e != nil {
			return "", e
		}
		candidate.TargetVersion = manifest.Version
		candidate.Source = install.UpgradeSourceDev
	case upgradeCommandLocal:
		if !install.CurrentBuildAllowsLocalUpgrade() {
			return "", fmt.Errorf("当前构建不支持本地产物升级")
		}
		candidate.LocalArtifact = install.LocalUpgradeArtifactPath(state)
		body, e := os.ReadFile(candidate.LocalArtifact)
		if e != nil {
			return "", e
		}
		sum := sha256.Sum256(body)
		candidate.LocalHash = hex.EncodeToString(sum[:])
		candidate.TargetVersion = "本地已构建产物"
	default:
		return "", fmt.Errorf("未知升级操作")
	}
	if strings.TrimSpace(candidate.TargetVersion) == "" {
		return "", fmt.Errorf("实际升级来源未返回目标版本")
	}
	contentID := teamID("upgrade-candidate", source.Key)
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		raw, e := json.Marshal(candidate)
		if e != nil {
			return e
		}
		return tx.PutContent(work.NewContent(contentID, source.ScopeID, "application/json", raw, time.Now().UTC()))
	}); err != nil {
		return "", err
	}
	panel := workui.Panel{Title: "确认主机升级", Paragraphs: []string{"目标：" + candidate.TargetVersion, "确认后会准备并执行真实升级，daemon 会短暂重启；当前工作与尚未处理的输入会按持久状态恢复。"}, Buttons: []workui.Button{{Label: "确认升级", Action: "host.upgrade.confirm", Values: map[string]any{"candidate_ref": contentID}}, {Label: "取消", Action: "host.upgrade.cancel"}}}
	return a.queueTeamSystemPanelKind(ctx, source, panel, "host.upgrade-confirm", contentID)
}
func (a *App) handleTeamHostViewAction(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) (string, error) {
	if view.Kind != "host.upgrade-confirm" {
		return "", workstore.ErrScopeDenied
	}
	var candidate teamUpgradeCandidate
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.ValidateViewInteraction(view, interaction); e != nil {
			return e
		}
		if action == "host.upgrade.cancel" {
			return nil
		}
		ref, _ := interaction.Envelope.Values["candidate_ref"].(string)
		if ref != view.TargetID {
			return workstore.ErrScopeDenied
		}
		body, e := tx.GetContent(view.ScopeID, ref)
		if e != nil {
			return e
		}
		return json.Unmarshal(body.Data, &candidate)
	})
	if err != nil {
		return "", err
	}
	source := teamHumanSource{Key: interaction.ID, ScopeID: view.ScopeID, PrincipalID: interaction.PrincipalID, RoleID: interaction.Gateway.RoleID, Tenant: view.Tenant, ChatID: view.ChatID, MessageID: interaction.Envelope.MessageID, Gateway: interaction.Gateway, SenderIDs: interaction.Envelope.SenderIDs, Authority: workstore.PrivateActionSource{PersonalScopeID: view.ScopeID, InteractionID: interaction.ID}}
	if action == "host.upgrade.cancel" {
		started, err := a.teamUpgradeAlreadyStarted(ctx, view)
		if err != nil {
			return "", err
		}
		if started {
			return a.queueTeamSystemPanel(ctx, source, workui.Panel{Title: "系统升级", Paragraphs: []string{"升级操作已有持久执行记录，或系统已有升级事务；本次返回没有撤销已登记的升级。请查看当前状态。"}, Buttons: []workui.Button{systemCommandButton("查看实际升级状态", "/upgrade")}})
		}
		return a.queueTeamSystemPanel(ctx, source, workui.Panel{Title: "已退出升级确认", Paragraphs: []string{"已退出本次确认，没有提交新的升级请求。已存在的升级事务不会被撤销，请查看当前状态。"}, Buttons: []workui.Button{systemCommandButton("查看升级状态", "/upgrade")}})
	}
	if action != "host.upgrade.confirm" {
		return "", workstore.ErrScopeDenied
	}
	return a.teamHostAction(ctx, source, "host.upgrade.execute", struct {
		Candidate teamUpgradeCandidate
		Source    workstore.PrivateActionSource
	}{candidate, source.Authority}, func() (string, error) {
		a.mu.Lock()
		fresh, _, e := a.loadUpgradeStateLocked(true)
		if e != nil {
			a.mu.Unlock()
			return "", e
		}
		if teamUpgradeFingerprint(fresh) != candidate.StateFingerprint || a.upgradeRuntime.StartInFlight || a.upgradeRuntime.CheckInFlight || pendingUpgradeBusy(fresh.PendingUpgrade) {
			a.mu.Unlock()
			return "", fmt.Errorf("安装状态已改变，请重新检查目标版本")
		}
		if candidate.Mode == upgradeCommandLocal {
			a.upgradeRuntime.StartInFlight = true
			a.mu.Unlock()
			defer func() { a.mu.Lock(); a.upgradeRuntime.StartInFlight = false; a.mu.Unlock() }()
			raw, e := os.ReadFile(candidate.LocalArtifact)
			if e != nil {
				return "", e
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != candidate.LocalHash {
				return "", fmt.Errorf("本地产物已改变，请重新确认")
			}
			slot, e := install.RunLocalBinaryUpgradeWithStatePath(install.LocalBinaryUpgradeOptions{StatePath: fresh.StatePath, SourceBinary: candidate.LocalArtifact})
			if e != nil {
				return "", e
			}
			return "本地升级准备已实际提交，目标 slot：" + slot + "。完成情况可通过 /upgrade 查询。", nil
		}
		now := time.Now().UTC()
		fresh.PendingUpgrade = &install.PendingUpgrade{Phase: install.PendingUpgradePhasePrompted, Source: candidate.Source, TargetVersion: candidate.TargetVersion, GatewayID: source.Gateway.GatewayRef, ChatID: source.ChatID, ActorUserID: source.SenderIDs.OpenID, SourceMessageID: source.MessageID, RequestedAt: &now}
		if e = a.writeUpgradeStateLocked(fresh); e != nil {
			a.mu.Unlock()
			return "", e
		}
		a.upgradeRuntime.StartInFlight = true
		a.mu.Unlock()
		go a.runPendingUpgradeStart(upgradeStartRequest{State: fresh, GatewayID: source.Gateway.GatewayRef, SourceMessageID: source.MessageID})
		return "升级事务已实际登记，正在准备目标版本。是否完成请查看 /upgrade 的持久升级状态。", nil
	})
}

func (a *App) teamUpgradeAlreadyStarted(ctx context.Context, view workstore.UIView) (bool, error) {
	a.mu.Lock()
	state, _, err := a.loadUpgradeStateLocked(false)
	inFlight := a.upgradeRuntime.StartInFlight
	a.mu.Unlock()
	if err != nil {
		return false, err
	}
	if inFlight || pendingUpgradeBusy(state.PendingUpgrade) {
		return true, nil
	}
	started := false
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		operations, err := tx.ListOperations(view.ScopeID)
		if err != nil {
			return err
		}
		for _, op := range operations {
			if op.Kind != "host.upgrade.execute" {
				continue
			}
			body, err := tx.GetContent(op.ScopeID, op.RequestRef)
			if err != nil {
				return err
			}
			var request struct{ Source workstore.PrivateActionSource }
			if err = json.Unmarshal(body.Data, &request); err != nil {
				return err
			}
			if request.Source.InteractionID == "" {
				continue
			}
			interaction, err := tx.GetHumanInteraction(request.Source.InteractionID)
			if err != nil {
				return err
			}
			if interaction.Envelope.Values["team_view"] == view.ID {
				started = true
				break
			}
		}
		return nil
	})
	return started, err
}
