package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamPrivateStatus struct {
	Navigation      work.PrivateNavigation
	Lines           []string
	Settings        worksettings.Snapshot
	Preferences     work.ContextPreferences
	SettingsProblem string
}

// Status reads the durable private owner, never the obsolete surface snapshot.
// Configuration and bounded Git inspection run outside the store transaction.
func (a *App) readTeamPrivateStatus(ctx context.Context, source teamHumanSource) (teamPrivateStatus, error) {
	var result teamPrivateStatus
	var binding work.RoleBotBinding
	var location work.ExecutionLocation
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := a.validateTeamPickerSource(tx, source); err != nil {
			return err
		}
		var err error
		result.Navigation, err = tx.GetPrivateNavigation(source.ScopeID)
		if err != nil {
			return err
		}
		wc, err := tx.GetContext(result.Navigation.FrontContextID)
		if err != nil {
			return err
		}
		facts, err := tx.ReadPrivateContextStatus(source.ScopeID, wc.ID, source.PrincipalID)
		if err != nil {
			return err
		}
		binding, err = tx.GetRoleBotBinding(source.RoleID)
		if err != nil {
			return err
		}
		result.Lines = []string{"当前会话：新会话，等待首条消息"}
		if facts.FirstUser != nil {
			preview, err := teamStatusMessagePreview(tx, *facts.FirstUser)
			if err != nil {
				return err
			}
			result.Lines[0] = "当前会话：" + preview
		}
		if facts.LastUser != nil {
			preview, err := teamStatusMessagePreview(tx, *facts.LastUser)
			if err != nil {
				return err
			}
			result.Lines = append(result.Lines, "最近输入："+preview, "最近输入时间："+facts.LastUser.CreatedAt.Format(time.RFC3339))
		}
		if wc.ProjectViewScopeID != "" {
			result.Lines = append(result.Lines, "讨论范围：与项目关联的私人会话")
		}
		result.Lines = append(result.Lines, fmt.Sprintf("消息队列：%d 条待处理，%d 条正在处理", facts.Pending, facts.Claimed))
		if facts.AwaitingResponse > 0 {
			result.Lines = append(result.Lines, fmt.Sprintf("等待你的答复：%d 项，请处理对应的确认或问题卡片", facts.AwaitingResponse))
		}
		preference, err := tx.GetExecutionPreference(source.ScopeID)
		if err == nil && preference.DefaultLocationID != "" {
			location, err = tx.GetLocation(preference.DefaultLocationID)
			if err != nil {
				return err
			}
			result.Lines = append(result.Lines, "所选工作目录："+location.Cwd)
			if len(preference.SelectedLocationIDs) > 1 {
				result.Lines = append(result.Lines, fmt.Sprintf("已授权目录：%d 个（/workspace 查看或修改）", len(preference.SelectedLocationIDs)))
			}
		} else if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		} else {
			result.Lines = append(result.Lines, "工作目录：尚未选择（/workspace）")
		}
		if err := appendTeamPrivateWorkerStatus(tx, wc, &result, &location); err != nil {
			return err
		}
		result.Preferences, err = tx.GetContextPreferences(wc.ID)
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		for _, item := range []struct {
			kind  work.AutomationKind
			label string
		}{{work.AutomationWhip, "自动检查未完成工作 /autowhip"}, {work.AutomationContinue, "断线后自动继续 /autocontinue"}} {
			policy, err := tx.GetAutomationPolicy(wc.ID, item.kind)
			if err != nil && !errors.Is(err, workstore.ErrNotFound) {
				return err
			}
			value := "关闭"
			if policy.Enabled {
				value = "开启"
			}
			result.Lines = append(result.Lines, item.label+"："+value)
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	result.Settings, err = (teamBotSettingsPort{app: a}).Snapshot(ctx, binding)
	if err != nil {
		// Keep the execution facts readable even when configuration itself is
		// broken; the page explains the failed read instead of inventing defaults.
		result.SettingsProblem = "参数配置读取失败：" + teamStatusPreview(err.Error(), 200)
	}
	if location.Cwd != "" {
		hostname, err := os.Hostname()
		if err != nil {
			return result, err
		}
		if location.HostID == teamID("host", hostname+":"+a.headlessRuntime.Paths.StateDir) {
			info, err := gitmeta.InspectWorkspace(location.Cwd, gitmeta.InspectOptions{IncludeStatus: true, Timeout: 750 * time.Millisecond})
			if err != nil {
				result.Lines = append(result.Lines, "Git 状态暂时无法读取："+teamStatusPreview(err.Error(), 160))
			} else if info.InRepo() {
				branch := info.Branch
				if info.Detached {
					branch = "游离提交"
				}
				result.Lines = append(result.Lines, "Git 分支："+branch, fmt.Sprintf("目录变更：%d 个已跟踪文件，%d 个未跟踪文件", info.Status.ModifiedCount, info.Status.UntrackedCount))
			}
		}
	}
	if binary, err := os.Executable(); err == nil {
		result.Lines = append(result.Lines, "当前程序："+binary)
	}
	gaps := a.snapshotFeishuPermissionGaps(source.Gateway.GatewayRef)
	for index, gap := range gaps {
		if index == 10 {
			result.Lines = append(result.Lines, fmt.Sprintf("已列出 10 项权限缺口，共 %d 项。", len(gaps)))
			break
		}
		result.Lines = append(result.Lines, "已观察到的飞书权限缺口："+gap.Scope+"（"+gap.SourceAPI+"）")
		if gap.ApplyURL != "" {
			result.Lines = append(result.Lines, "权限申请："+gap.ApplyURL)
		}
	}
	return result, nil
}

func teamStatusMessagePreview(tx *workstore.Tx, message work.Message) (string, error) {
	body, err := tx.GetContent(message.ScopeID, message.BodyRef)
	if err != nil {
		return "", err
	}
	text := teamStatusPreview(string(body.Data), 80)
	if text == "" && len(message.Attachments) > 0 {
		text = "附件消息"
	}
	return text, nil
}

func teamStatusPreview(text string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}
