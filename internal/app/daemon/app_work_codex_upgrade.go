package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workupgrade"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamCodexUpgrader interface {
	Request(context.Context, workupgrade.Request) (workupgrade.Record, error)
	Status(context.Context, string) (workupgrade.Record, error)
	Run(context.Context, int) error
}

type teamCodexUpgradeCandidate struct {
	TargetVersion           string `json:"target_version"`
	InstallationFingerprint string `json:"installation_fingerprint"`
}

func (a *App) queueTeamCodexUpgrade(ctx context.Context, source teamHumanSource) (string, error) {
	r := a.teamRuntime
	if r.codexUpgrader == nil {
		return "", fmt.Errorf("Codex 程序维护入口尚未就绪")
	}
	var active workstore.NativeUpgrade
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		var e error
		active, e = tx.ActiveNativeUpgrade(r.codexUpgradeHostID)
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		return e
	})
	if err != nil {
		return "", err
	}
	if active.ID != "" {
		if active.Source.PersonalScopeID != source.ScopeID {
			return a.queueTeamPicker(ctx, source, "host.codex-upgrade.info", source.Key, 1, workui.Panel{Title: "Codex 程序正在维护", Paragraphs: []string{"已有一项主机维护正在进行，由原发起者查看进度。尚未处理的工作仍会保留，当前不会启动另一项安装。"}})
		}
		return a.queueTeamPicker(ctx, source, "host.codex-upgrade.status", active.ID, active.Revision, teamCodexUpgradePanel(active))
	}
	queryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	check, err := a.checkStandaloneCodexUpgrade(queryCtx, "")
	cancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("Codex 版本查询超时，尚未启动维护；可以稍后重试")
		}
		return "", err
	}
	if !check.Installation.Upgradeable() || check.LatestVersion == "" {
		reason := check.Installation.Problem
		if check.Installation.BundleBacked() {
			reason = "当前 Codex 来自 VS Code 安装包，需要通过对应安装来源更新。"
		}
		if reason == "" {
			reason = "当前安装来源没有可用的自动更新版本。"
		}
		return a.queueTeamPicker(ctx, source, "host.codex-upgrade.info", source.Key, 1, workui.Panel{Title: "Codex 程序版本", Paragraphs: []string{"当前安装：" + check.CurrentVersion, reason, "本次没有更新程序。"}})
	}
	candidate := teamCodexUpgradeCandidate{TargetVersion: check.LatestVersion, InstallationFingerprint: workupgrade.InstallationFingerprint(check.Installation)}
	ref := teamID("codex-upgrade-candidate", source.Key)
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		body, e := json.Marshal(candidate)
		if e != nil {
			return e
		}
		return tx.PutContent(work.NewContent(ref, source.ScopeID, "application/json", body, time.Now().UTC()))
	})
	if err != nil {
		return "", err
	}
	return a.queueTeamPicker(ctx, source, "host.codex-upgrade.confirm", ref, 1, workui.Panel{Title: "确认更新 Codex 程序", Paragraphs: []string{"当前安装：" + check.CurrentVersion, "目标版本：" + candidate.TargetVersion, "系统会先停止本机受管的 Codex 执行，确认进程退出，再安装并验证实际使用的程序版本。未处理的输入和工作记录会保留；Claude 和其他后端不受这个维护流程影响。"}, Buttons: []workui.Button{{Label: "确认更新", Action: "host.codex-upgrade.confirm"}, {Label: "取消", Action: "host.codex-upgrade.cancel"}}})
}

func teamCodexUpgradePanel(record workupgrade.Record) workui.Panel {
	stage := map[string]string{"draining": "正在停止受管 Codex 执行", "installing_global": "正在安装选定版本", "installing_private": "正在准备受管运行程序", "verifying": "正在核对实际安装", "restarting": "正在恢复受管连接", "completed": "更新已完成", "unknown": "更新结果仍待核查"}[record.Stage]
	if stage == "" {
		stage = "正在核对维护进度"
	}
	paragraphs := []string{"目标版本：" + record.TargetVersion, stage}
	if record.ManagedVersion != "" {
		paragraphs = append(paragraphs, "已核实的受管程序版本："+record.ManagedVersion)
	}
	if record.LastError != "" {
		paragraphs = append(paragraphs, record.LastError)
	}
	if record.Stage == "unknown" {
		paragraphs = append(paragraphs, "结果不确定时不会重复运行安装，也不会恢复未经确认的 Codex 执行。系统会继续核查已有进程和安装证据。")
	}
	if record.Stage == "completed" {
		paragraphs = append(paragraphs, "未处理的工作可按原有调度继续；原先的暂停或其他恢复限制不会被这次更新解除。")
	}
	return workui.Panel{Title: "Codex 程序维护", Paragraphs: paragraphs, Buttons: []workui.Button{{Label: "刷新进度", Action: "host.codex-upgrade.refresh"}}}
}

func (a *App) handleTeamCodexUpgradeView(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	r := a.teamRuntime
	if r.codexUpgrader == nil {
		return fmt.Errorf("Codex 程序维护入口尚未就绪")
	}
	var candidate teamCodexUpgradeCandidate
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.ValidateViewInteraction(view, interaction); e != nil {
			return e
		}
		if view.Kind == "host.codex-upgrade.confirm" && action == "host.codex-upgrade.confirm" {
			body, e := tx.GetContent(view.ScopeID, view.TargetID)
			if e != nil {
				return e
			}
			return json.Unmarshal(body.Data, &candidate)
		}
		return nil
	})
	if err != nil {
		return err
	}
	source := teamHumanSource{Key: interaction.ID, ScopeID: view.ScopeID, PrincipalID: interaction.PrincipalID, RoleID: interaction.Gateway.RoleID, Tenant: view.Tenant, ChatID: view.ChatID, MessageID: interaction.Envelope.MessageID, Gateway: interaction.Gateway, SenderIDs: interaction.Envelope.SenderIDs, Authority: workstore.PrivateActionSource{PersonalScopeID: view.ScopeID, InteractionID: interaction.ID}}
	result := ""
	switch {
	case view.Kind == "host.codex-upgrade.confirm" && action == "host.codex-upgrade.cancel":
		record, lookup := r.codexUpgrader.Status(ctx, teamID("codex-upgrade", view.TargetID))
		if lookup == nil {
			if record.Source.PersonalScopeID != view.ScopeID {
				return workstore.ErrScopeDenied
			}
			result, err = a.queueTeamPicker(ctx, source, "host.codex-upgrade.status", record.ID, record.Revision, teamCodexUpgradePanel(record))
		} else if errors.Is(lookup, workstore.ErrNotFound) {
			result, err = a.queueTeamPicker(ctx, source, "host.result", view.TargetID, 1, workui.Panel{Title: "更新已取消", Paragraphs: []string{"没有启动 Codex 程序更新。"}})
		} else {
			err = lookup
		}
	case view.Kind == "host.codex-upgrade.confirm" && action == "host.codex-upgrade.confirm":
		var record workupgrade.Record
		record, err = r.codexUpgrader.Request(ctx, workupgrade.Request{ID: teamID("codex-upgrade", view.TargetID), TargetVersion: candidate.TargetVersion, InstallationFingerprint: candidate.InstallationFingerprint, Source: source.Authority})
		if err == nil {
			result, err = a.queueTeamPicker(ctx, source, "host.codex-upgrade.status", record.ID, record.Revision, teamCodexUpgradePanel(record))
		}
	case (view.Kind == "host.codex-upgrade.status" || view.Kind == "host.codex-upgrade.result") && action == "host.codex-upgrade.refresh":
		var record workupgrade.Record
		record, err = r.codexUpgrader.Status(ctx, view.TargetID)
		if err == nil && record.Source.PersonalScopeID != view.ScopeID {
			err = workstore.ErrScopeDenied
		}
		if err == nil {
			result, err = a.queueTeamPicker(ctx, source, "host.codex-upgrade.status", record.ID, record.Revision, teamCodexUpgradePanel(record))
		}
	default:
		return workstore.ErrScopeDenied
	}
	if err != nil {
		return err
	}
	return r.store.Within(ctx, func(tx *workstore.Tx) error {
		if view.Kind == "host.codex-upgrade.confirm" {
			current, e := tx.GetUIView(view.ID)
			if e != nil {
				return e
			}
			if !current.Invalidated {
				expected := current.Revision
				current.Revision++
				current.Invalidated = true
				if e = tx.PutUIView(current, expected); e != nil {
					return e
				}
			}
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, result)
	})
}
