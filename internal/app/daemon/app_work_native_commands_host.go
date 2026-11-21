package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) teamHostAction(ctx context.Context, source teamHumanSource, kind string, request any, execute func() (string, error)) (string, error) {
	id := teamID("human-host", source.Key+":"+kind)
	send := false
	result := ""
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		raw, e := json.Marshal(request)
		if e != nil {
			return e
		}
		body := work.NewContent(id+":request", source.ScopeID, "application/json", raw, time.Now().UTC())
		if old, e := tx.GetOperation(id); e == nil {
			if old.RequestHash != body.SHA256 {
				return workstore.ErrIdempotencyConflict
			}
			if old.State == work.OperationConfirmed {
				result = old.ExternalReceipt
				return nil
			}
			return fmt.Errorf("这次主机操作已经受理，结果仍待核对；不会自动重复执行")
		} else if e != workstore.ErrNotFound {
			return e
		}
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: id, ScopeID: source.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, Kind: kind, TargetID: "installation", TargetGeneration: 1, IdempotencyKey: source.Key, RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, 0)
		send = e == nil
		return e
	})
	if err != nil {
		return "", err
	}
	if send {
		result, err = execute()
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		saveErr := a.teamRuntime.store.Within(saveCtx, func(tx *workstore.Tx) error {
			op, e := tx.GetOperation(id)
			if e != nil {
				return e
			}
			previous := op.Revision
			op.Revision++
			if err != nil {
				op.State = work.OperationUnknown
				op.LastError = err.Error()
			} else {
				op.State = work.OperationConfirmed
				op.ExternalReceipt = result
			}
			op.UpdatedAt = time.Now().UTC()
			_, _, e = tx.PutOperation(op, previous)
			return e
		})
		cancel()
		if saveErr != nil {
			return "", saveErr
		}
		if err != nil {
			return "", err
		}
	}
	return a.queueTeamPicker(ctx, source, "host.result", "installation", 1, workui.Panel{Title: "主机管理", Paragraphs: []string{result}})
}
func (a *App) handleTeamAdminCommand(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	text := strings.TrimSpace(action.Text)
	if text == "/admin" {
		text = "/admin web"
	}
	parsed, err := parseAdminCommandText(text)
	if err != nil {
		return "", err
	}
	return a.teamHostAction(ctx, source, "host.admin."+string(parsed.Mode), struct {
		Text   string
		Source workstore.PrivateActionSource
	}{text, source.Authority}, func() (string, error) {
		switch parsed.Mode {
		case adminCommandWeb:
			a.mu.Lock()
			mode := a.externalAccessNetworkModeLocked()
			local := a.localAdminURLLocked()
			if mode == "local" {
				a.mu.Unlock()
				return "本机管理页：" + local, nil
			}
			service, localURL, e := a.ensureExternalAccessIssueTargetLocked()
			request := debugAdminIssueRequest(a.admin.adminURL)
			a.mu.Unlock()
			if e != nil {
				return "", e
			}
			issued, e := service.IssueURL(ctx, request, localURL)
			if e != nil {
				return "", e
			}
			return fmt.Sprintf("临时管理页：%s\n有效期至 %s", issued.ExternalURL, issued.ExpiresAt.UTC().Format(time.RFC3339)), nil
		case adminCommandNetwork:
			a.mu.Lock()
			mode := a.externalAccessNetworkModeLocked()
			a.mu.Unlock()
			return "当前网络模式：" + mode, nil
		case adminCommandNetworkSet:
			if e := a.applyExternalAccessNetworkMode(parsed.NetworkMode); e != nil {
				return "", e
			}
			a.mu.Lock()
			mode := a.externalAccessNetworkModeLocked()
			a.mu.Unlock()
			return "网络模式已实际保存为：" + mode, nil
		case adminCommandAutostart, adminCommandAutostartOn, adminCommandAutostartOff:
			var status install.AutostartStatus
			var e error
			switch parsed.Mode {
			case adminCommandAutostart:
				status, e = detectAutostart(a.installStatePath())
			case adminCommandAutostartOff:
				status, e = disableAutostart(a.installStatePath())
			default:
				binary, e := a.currentBinaryPath()
				if e != nil {
					return "", e
				}
				status, e = applyAutostart(install.AutostartApplyOptions{StatePath: a.installStatePath(), InstalledBinary: binary, CurrentVersion: a.currentBinaryVersion()})
			}
			if e != nil {
				return "", e
			}
			return fmt.Sprintf("自动启动状态：%s\n已启用：%t\n%s", status.Status, status.Enabled, status.Warning), nil
		}
		return "", fmt.Errorf("未知主机管理操作")
	})
}
func (a *App) teamUpgradeStatus(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	parsed, err := parseUpgradeCommandText(action.Text)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	state, _, err := a.loadUpgradeStateLocked(parsed.Mode != upgradeCommandShowStatus && parsed.Mode != upgradeCommandShowTrack)
	a.mu.Unlock()
	if err != nil {
		return "", err
	}
	if parsed.Mode != upgradeCommandShowStatus && parsed.Mode != upgradeCommandShowTrack {
		return a.teamUpgradeAction(ctx, source, parsed, state)
	}
	note := fmt.Sprintf("当前版本：%s\n发布轨道：%s\n已知最新版本：%s", a.currentBinaryVersion(), state.CurrentTrack, state.LastKnownLatestVersion)
	if state.PendingUpgrade != nil {
		note += fmt.Sprintf("\n持久升级事务：%s，目标 %s", state.PendingUpgrade.Phase, state.PendingUpgrade.TargetVersion)
	}
	return a.queueTeamPicker(ctx, source, "host.upgrade-status", "installation", 1, workui.Panel{Title: "升级状态", Paragraphs: []string{note}})
}
