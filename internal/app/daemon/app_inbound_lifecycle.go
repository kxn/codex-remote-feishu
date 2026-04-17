package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const oldInboundActionWindow = 2 * time.Minute

func daemonLifecycleID(identity agentproto.ServerIdentity, startedAt time.Time) string {
	startedAt = startedAt.UTC()
	switch {
	case !startedAt.IsZero() && identity.PID > 0:
		return fmt.Sprintf("%s|pid:%d", startedAt.Format(time.RFC3339Nano), identity.PID)
	case !startedAt.IsZero():
		return startedAt.Format(time.RFC3339Nano)
	case identity.PID > 0:
		return fmt.Sprintf("pid:%d", identity.PID)
	default:
		return "unknown"
	}
}

func (a *App) classifyInboundAction(action control.Action) control.Action {
	if action.Inbound == nil {
		action.Inbound = &control.ActionInboundMeta{}
	}
	meta := action.Inbound
	cardLifecycleID := strings.TrimSpace(meta.CardDaemonLifecycleID)
	if cardLifecycleID != "" && cardLifecycleID != strings.TrimSpace(a.daemonLifecycleID) {
		meta.LifecycleVerdict = control.InboundLifecycleOldCard
		meta.LifecycleReason = "card_lifecycle_mismatch"
		return action
	}

	switch {
	case a.inboundTimeIsOld(meta.MessageCreateTime):
		meta.LifecycleVerdict = control.InboundLifecycleOld
		meta.LifecycleReason = "message_before_start_window"
	case a.inboundTimeIsOld(meta.MenuClickTime):
		meta.LifecycleVerdict = control.InboundLifecycleOld
		meta.LifecycleReason = "menu_before_start_window"
	default:
		meta.LifecycleVerdict = control.InboundLifecycleCurrent
		meta.LifecycleReason = ""
	}
	return action
}

func (a *App) inboundTimeIsOld(ts time.Time) bool {
	if ts.IsZero() || a.daemonStartedAt.IsZero() {
		return false
	}
	return !ts.After(a.daemonStartedAt.Add(-oldInboundActionWindow))
}

func inboundVerdict(action control.Action) control.InboundLifecycleVerdict {
	if action.Inbound == nil {
		return control.InboundLifecycleCurrent
	}
	if action.Inbound.LifecycleVerdict == "" {
		return control.InboundLifecycleCurrent
	}
	return action.Inbound.LifecycleVerdict
}

func inboundReason(action control.Action) string {
	if action.Inbound == nil {
		return ""
	}
	return strings.TrimSpace(action.Inbound.LifecycleReason)
}

func inboundTimeValue(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func rejectedInboundNotice(action control.Action) *control.Notice {
	switch inboundVerdict(action) {
	case control.InboundLifecycleOld:
		text := "检测到这是服务上一个生命周期里的旧消息、旧命令或旧菜单动作，已忽略。"
		if detail := rejectedInboundActionDetail(action); detail != "" {
			text += "\n\n本次被忽略的是：" + detail + "。"
		}
		text += "\n\n请重新发送消息、命令或重新点击菜单。"
		return &control.Notice{
			Code:     "old_inbound_ignored",
			Title:    "旧动作已忽略",
			Text:     text,
			ThemeKey: "error",
		}
	case control.InboundLifecycleOldCard:
		text := "这张卡片来自服务上一个生命周期，已过期。"
		if detail := rejectedInboundActionDetail(action); detail != "" {
			text += "\n\n卡片对应动作：" + detail + "。"
		}
		text += "\n\n请重新发送对应命令获取新卡片。"
		return &control.Notice{
			Code:     "old_card_expired",
			Title:    "旧卡片已过期",
			Text:     text,
			ThemeKey: "error",
		}
	default:
		return nil
	}
}

func rejectedInboundActionDetail(action control.Action) string {
	switch action.Kind {
	case control.ActionTextMessage:
		if preview := compactActionPreview(action.Text, 48); preview != "" {
			return fmt.Sprintf("消息“%s”", preview)
		}
		return "一条消息"
	case control.ActionImageMessage:
		return "一张图片消息"
	case control.ActionReactionCreated:
		return "一条消息表情操作"
	case control.ActionMessageRecalled:
		return "一条消息撤回操作"
	}

	if command := explicitActionCommand(action); command != "" {
		return fmt.Sprintf("命令“%s”", command)
	}

	label, command := rejectedInboundActionLabel(action)
	switch {
	case label != "" && command != "":
		return fmt.Sprintf("%s（对应 %s）", label, command)
	case label != "":
		return label
	case command != "":
		return fmt.Sprintf("命令“%s”", command)
	default:
		return ""
	}
}

func explicitActionCommand(action control.Action) string {
	text := strings.TrimSpace(action.Text)
	if text == "" {
		return ""
	}
	switch action.Kind {
	case control.ActionTextMessage, control.ActionImageMessage, control.ActionReactionCreated, control.ActionMessageRecalled:
		return ""
	case control.ActionRemovedCommand:
		return control.LegacyActionCommand(text)
	}
	return text
}

func rejectedInboundActionLabel(action control.Action) (label, command string) {
	if intent, ok := control.FeishuUIIntentFromAction(action); ok {
		return rejectedInboundIntentLabel(*intent)
	}
	switch action.Kind {
	case control.ActionListInstances:
		return "查看实例", "/list"
	case control.ActionStatus:
		return "查看状态", "/status"
	case control.ActionStop:
		return "停止", "/stop"
	case control.ActionNewThread:
		return "新建会话", "/new"
	case control.ActionKillInstance:
		return "解除接管", "/detach"
	case control.ActionRemovedCommand:
		return "已移除命令", control.LegacyActionCommand(action.Text)
	case control.ActionShowCommandHelp:
		return "查看帮助", "/help"
	case control.ActionDebugCommand:
		return "查看调试升级状态", "/debug"
	case control.ActionUpgradeCommand:
		return "发起升级", "/upgrade"
	case control.ActionModelCommand:
		return "设置下一条消息模型", "/model"
	case control.ActionReasoningCommand:
		return "设置下一条消息推理强度", "/reasoning"
	case control.ActionAccessCommand:
		return "设置下一条消息执行权限", "/access"
	case control.ActionRespondRequest:
		return "响应授权请求", ""
	case control.ActionSelectPrompt:
		return "选择提示卡动作", ""
	case control.ActionAttachInstance:
		return "接管实例", "/list"
	case control.ActionUseThread:
		return "切换会话", "/use"
	case control.ActionConfirmKickThread:
		return "确认强踢会话", "/use"
	case control.ActionCancelKickThread:
		return "取消强踢会话", "/use"
	case control.ActionFollowLocal:
		return "跟随当前", "/follow"
	case control.ActionDetach:
		return "解除接管", "/detach"
	case control.ActionPathPickerEnter, control.ActionPathPickerUp, control.ActionPathPickerSelect, control.ActionPathPickerConfirm, control.ActionPathPickerCancel:
		return "路径选择器卡片动作", ""
	case control.ActionTargetPickerSelectMode,
		control.ActionTargetPickerSelectSource,
		control.ActionTargetPickerSelectWorkspace,
		control.ActionTargetPickerSelectSession,
		control.ActionTargetPickerOpenPathPicker,
		control.ActionTargetPickerCancel,
		control.ActionTargetPickerConfirm:
		return "目标选择卡片动作", ""
	default:
		return "", ""
	}
}

func rejectedInboundIntentLabel(intent control.FeishuUIIntent) (label, command string) {
	switch intent.Kind {
	case control.FeishuUIIntentShowCommandMenu:
		return "打开命令菜单", "/menu"
	case control.FeishuUIIntentShowModeCatalog:
		return "查看模式设置", "/mode"
	case control.FeishuUIIntentShowAutoContinueCatalog:
		return "查看 AutoWhip 设置", "/autowhip"
	case control.FeishuUIIntentShowReasoningCatalog:
		return "查看推理强度设置", "/reasoning"
	case control.FeishuUIIntentShowAccessCatalog:
		return "查看执行权限设置", "/access"
	case control.FeishuUIIntentShowModelCatalog:
		return "查看模型设置", "/model"
	case control.FeishuUIIntentShowRecentWorkspaces, control.FeishuUIIntentShowAllWorkspaces:
		return "查看工作区列表", "/list"
	case control.FeishuUIIntentShowThreads, control.FeishuUIIntentShowScopedThreads:
		return "查看会话列表", "/use"
	case control.FeishuUIIntentShowAllThreads,
		control.FeishuUIIntentShowAllThreadWorkspaces,
		control.FeishuUIIntentShowRecentThreadWorkspaces:
		return "查看全部会话", "/useall"
	case control.FeishuUIIntentShowWorkspaceThreads:
		return "展开该工作区下的会话列表", ""
	case control.FeishuUIIntentPathPickerEnter:
		return "进入目录", ""
	case control.FeishuUIIntentPathPickerUp:
		return "返回上一级目录", ""
	case control.FeishuUIIntentPathPickerSelect:
		return "选择路径", ""
	case control.FeishuUIIntentTargetPickerSelectWorkspace:
		return "切换工作区候选", ""
	case control.FeishuUIIntentTargetPickerSelectSession:
		return "切换会话候选", ""
	case control.FeishuUIIntentTargetPickerOpenPathPicker:
		return "打开目录选择子步骤", ""
	case control.FeishuUIIntentTargetPickerCancel:
		return "取消目标选择", ""
	case control.FeishuUIIntentPathPickerConfirm:
		return "确认路径选择", ""
	case control.FeishuUIIntentPathPickerCancel:
		return "取消路径选择", ""
	default:
		return "", ""
	}
}

func compactActionPreview(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" || limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit == 1 {
		return string(runes[:1])
	}
	return string(runes[:limit-1]) + "…"
}
