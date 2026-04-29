package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type FeishuCommandStrategyKind string

const (
	FeishuCommandStrategyNative        FeishuCommandStrategyKind = "native"
	FeishuCommandStrategyApproximation FeishuCommandStrategyKind = "approximation"
	FeishuCommandStrategyPassthrough   FeishuCommandStrategyKind = "passthrough"
	FeishuCommandStrategyReject        FeishuCommandStrategyKind = "reject"
)

type FeishuCommandStrategy struct {
	FamilyID        string
	Backend         agentproto.Backend
	Kind            FeishuCommandStrategyKind
	Visible         bool
	DispatchAllowed bool
	Note            string
}

func ResolveFeishuCommandStrategy(ctx CatalogContext, familyID string) (FeishuCommandStrategy, bool) {
	ctx = NormalizeCatalogContext(ctx)
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return FeishuCommandStrategy{}, false
	}
	if _, ok := FeishuCommandDefinitionByID(familyID); !ok {
		return FeishuCommandStrategy{}, false
	}
	if ctx.Backend != agentproto.BackendClaude {
		return FeishuCommandStrategy{
			FamilyID:        familyID,
			Backend:         ctx.Backend,
			Kind:            FeishuCommandStrategyNative,
			Visible:         true,
			DispatchAllowed: true,
		}, true
	}
	return resolveClaudeCommandStrategy(familyID), true
}

func ResolveFeishuActionStrategy(ctx CatalogContext, action Action) (FeishuCommandStrategy, bool) {
	ctx = NormalizeCatalogContext(ctx)
	if familyID := strings.TrimSpace(action.CatalogFamilyID); familyID != "" {
		return ResolveFeishuCommandStrategy(ctx, familyID)
	}
	if resolved, ok := ResolveFeishuActionCatalog(ctx, action); ok {
		return ResolveFeishuCommandStrategy(ctx, resolved.FamilyID)
	}
	return FeishuCommandStrategy{}, false
}

func resolveClaudeCommandStrategy(familyID string) FeishuCommandStrategy {
	switch familyID {
	case FeishuCommandStop,
		FeishuCommandStatus,
		FeishuCommandHistory,
		FeishuCommandModel,
		FeishuCommandReasoning,
		FeishuCommandAccess,
		FeishuCommandClaudeProfile,
		FeishuCommandMode,
		FeishuCommandVerbose,
		FeishuCommandHelp,
		FeishuCommandMenu,
		FeishuCommandDebug,
		FeishuCommandUpgrade:
		return claudeVisibleNativeStrategy(familyID)
	case FeishuCommandWorkspaceDetach:
		return claudeHiddenNativeStrategy(familyID, "当前 backend 仍允许本地解除接管，但暂不作为 Claude 可见主入口。")
	case FeishuCommandCompact:
		return claudeHiddenBlockedStrategy(familyID, FeishuCommandStrategyPassthrough, "Claude `/compact` 目前只作为后续 passthrough 候选；在 runtime host 收口前保持隐藏并拒绝直接执行。")
	case FeishuCommandNew,
		FeishuCommandList,
		FeishuCommandUse:
		return claudeVisibleAllowedStrategy(familyID, FeishuCommandStrategyApproximation, "Claude 会话切换沿用现有产品壳，但底层改走 backend-aware session catalog 与 route contract。")
	case FeishuCommandReview:
		return claudeVisibleAllowedStrategy(familyID, FeishuCommandStrategyApproximation, "Claude review 入口沿用现有 detached review 壳，底层继续复用当前 attached 会话上下文。")
	case FeishuCommandPatch:
		return claudeVisibleAllowedStrategy(familyID, FeishuCommandStrategyApproximation, "Claude turn patch 入口沿用现有 patch owner-flow 壳，底层继续复用当前 attached 会话上下文。")
	case FeishuCommandWorkspace,
		FeishuCommandWorkspaceList,
		FeishuCommandWorkspaceNew,
		FeishuCommandWorkspaceNewDir,
		FeishuCommandWorkspaceNewGit,
		FeishuCommandWorkspaceNewWorktree,
		FeishuCommandUseAll:
		return claudeHiddenBlockedStrategy(familyID, FeishuCommandStrategyApproximation, "Claude 当前 visible MVP 只开放 `/new` `/list` `/use` 这组会话主链；其余工作区/跨工作区入口继续保持隐藏并拒绝直接执行。")
	case FeishuCommandSteerAll:
		return claudeHiddenBlockedStrategy(familyID, FeishuCommandStrategyReject, "Claude 当前不支持 same-turn steer；请等待本轮结束后继续发送，或使用 /stop 中断。")
	case FeishuCommandPlan:
		return claudeHiddenBlockedStrategy(familyID, FeishuCommandStrategyReject, "Claude 计划确认走 request bridge；在显式 plan contract 落地前不开放 `/plan` 命令入口。")
	case FeishuCommandAutoWhip,
		FeishuCommandAutoContinue,
		FeishuCommandSendFile,
		FeishuCommandFollow,
		FeishuCommandDetach,
		FeishuCommandCron,
		FeishuCommandVSCodeMigrate:
		return claudeHiddenBlockedStrategy(familyID, FeishuCommandStrategyReject, "当前 Claude pre-MVP 范围未开放该命令；保持隐藏并显式拒绝。")
	default:
		return claudeHiddenBlockedStrategy(familyID, FeishuCommandStrategyReject, "当前 Claude pre-MVP 范围未开放该命令；保持隐藏并显式拒绝。")
	}
}

func claudeVisibleNativeStrategy(familyID string) FeishuCommandStrategy {
	return FeishuCommandStrategy{
		FamilyID:        familyID,
		Backend:         agentproto.BackendClaude,
		Kind:            FeishuCommandStrategyNative,
		Visible:         true,
		DispatchAllowed: true,
	}
}

func claudeVisibleAllowedStrategy(familyID string, kind FeishuCommandStrategyKind, note string) FeishuCommandStrategy {
	return FeishuCommandStrategy{
		FamilyID:        familyID,
		Backend:         agentproto.BackendClaude,
		Kind:            kind,
		Visible:         true,
		DispatchAllowed: true,
		Note:            strings.TrimSpace(note),
	}
}

func claudeHiddenNativeStrategy(familyID, note string) FeishuCommandStrategy {
	return FeishuCommandStrategy{
		FamilyID:        familyID,
		Backend:         agentproto.BackendClaude,
		Kind:            FeishuCommandStrategyNative,
		Visible:         false,
		DispatchAllowed: true,
		Note:            strings.TrimSpace(note),
	}
}

func claudeHiddenBlockedStrategy(familyID string, kind FeishuCommandStrategyKind, note string) FeishuCommandStrategy {
	return FeishuCommandStrategy{
		FamilyID:        familyID,
		Backend:         agentproto.BackendClaude,
		Kind:            kind,
		Visible:         false,
		DispatchAllowed: false,
		Note:            strings.TrimSpace(note),
	}
}
