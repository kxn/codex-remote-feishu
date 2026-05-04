package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type FeishuCommandSupportKind string

const (
	FeishuCommandSupportNative        FeishuCommandSupportKind = "native"
	FeishuCommandSupportApproximation FeishuCommandSupportKind = "approximation"
	FeishuCommandSupportPassthrough   FeishuCommandSupportKind = "passthrough"
	FeishuCommandSupportReject        FeishuCommandSupportKind = "reject"
)

type FeishuCommandDisplayProfile struct {
	VisibleMode            string
	DefaultDispatchAllowed bool
	DefaultSupportKind     FeishuCommandSupportKind
	DefaultRejectNote      string
	Families               map[string]FeishuCommandDisplayFamilyProfile
}

type FeishuCommandDisplayFamilyProfile struct {
	FamilyID        string
	Visible         bool
	MenuStages      map[FeishuCommandMenuStage]struct{}
	DispatchAllowed bool
	SupportKind     FeishuCommandSupportKind
	Note            string
}

type FeishuCommandSupport struct {
	FamilyID        string
	Backend         agentproto.Backend
	Kind            FeishuCommandSupportKind
	Visible         bool
	DispatchAllowed bool
	Note            string
}

var feishuCommandDisplayProfiles = map[string]FeishuCommandDisplayProfile{
	"codex": newFeishuCommandDisplayProfile("codex", true,
		commandSupportVisible(FeishuCommandStop),
		commandSupportVisible(FeishuCommandCompact),
		commandSupportVisible(FeishuCommandSteerAll),
		commandSupportVisibleWithStages(FeishuCommandNew, FeishuCommandMenuStageNormalWorking),
		commandSupportVisible(FeishuCommandStatus),
		commandSupportVisible(FeishuCommandReasoning),
		commandSupportVisible(FeishuCommandModel),
		commandSupportVisible(FeishuCommandAccess),
		commandSupportVisible(FeishuCommandPlan),
		commandSupportVisible(FeishuCommandVerbose),
		commandSupportVisible(FeishuCommandCodexProvider),
		commandSupportVisible(FeishuCommandAutoContinue),
		commandSupportVisible(FeishuCommandWorkspace),
		commandSupportVisible(FeishuCommandWorkspaceList),
		commandSupportVisible(FeishuCommandWorkspaceNew),
		commandSupportVisible(FeishuCommandWorkspaceNewDir),
		commandSupportVisible(FeishuCommandWorkspaceNewGit),
		commandSupportVisible(FeishuCommandWorkspaceNewWorktree),
		commandSupportVisible(FeishuCommandWorkspaceDetach),
		commandSupportVisible(FeishuCommandAutoWhip),
		commandSupportVisible(FeishuCommandHistory),
		commandSupportVisible(FeishuCommandReview),
		commandSupportVisible(FeishuCommandCron),
		commandSupportVisible(FeishuCommandSendFile),
		commandSupportVisible(FeishuCommandMode),
		commandSupportVisible(FeishuCommandUpgrade),
		commandSupportVisibleWithStages(FeishuCommandPatch, FeishuCommandMenuStageNormalWorking),
		commandSupportVisible(FeishuCommandDebug),
		commandSupportVisible(FeishuCommandHelp),
		commandSupportVisible(FeishuCommandMenu),
		commandSupportHiddenReject(FeishuCommandClaudeProfile, FeishuCommandSupportReject, "当前不在 Claude 模式，暂时不能切换 Claude 配置。请先 `/mode claude`。"),
	),
	"claude": newFeishuCommandDisplayProfile("claude", false,
		commandSupportVisible(FeishuCommandStop),
		commandSupportVisibleAs(FeishuCommandNew, FeishuCommandSupportApproximation, "Claude 会话切换沿用现有产品壳，但底层走 backend-aware session catalog 与 route contract。"),
		commandSupportVisible(FeishuCommandStatus),
		commandSupportVisible(FeishuCommandDetach),
		commandSupportVisible(FeishuCommandReasoning),
		commandSupportHiddenReject(FeishuCommandModel, FeishuCommandSupportReject, "Claude 模型请在 Claude 配置里设置，当前飞书会话不支持临时切换模型。"),
		commandSupportVisible(FeishuCommandAccess),
		commandSupportVisibleAs(FeishuCommandList, FeishuCommandSupportApproximation, "Claude 会话切换沿用现有产品壳，但底层走 backend-aware session catalog 与 route contract。"),
		commandSupportVisibleAs(FeishuCommandUse, FeishuCommandSupportApproximation, "Claude 会话切换沿用现有产品壳，但底层走 backend-aware session catalog 与 route contract。"),
		commandSupportVisible(FeishuCommandVerbose),
		commandSupportVisible(FeishuCommandHistory),
		commandSupportVisible(FeishuCommandSendFile),
		commandSupportVisible(FeishuCommandMode),
		commandSupportVisible(FeishuCommandClaudeProfile),
		commandSupportVisible(FeishuCommandUpgrade),
		commandSupportVisible(FeishuCommandDebug),
		commandSupportVisible(FeishuCommandHelp),
		commandSupportVisible(FeishuCommandMenu),
		commandSupportHiddenAllowed(FeishuCommandWorkspace),
		commandSupportHiddenAllowed(FeishuCommandWorkspaceList),
		commandSupportHiddenAllowed(FeishuCommandWorkspaceNew),
		commandSupportHiddenAllowed(FeishuCommandWorkspaceNewDir),
		commandSupportHiddenAllowed(FeishuCommandWorkspaceNewGit),
		commandSupportHiddenAllowed(FeishuCommandWorkspaceNewWorktree),
		commandSupportHiddenAllowed(FeishuCommandWorkspaceDetach),
		commandSupportHiddenReject(FeishuCommandCompact, FeishuCommandSupportPassthrough, "Claude `/compact` 目前只作为后续 passthrough 候选；在 runtime host 收口前保持隐藏并拒绝直接执行。"),
		commandSupportHiddenReject(FeishuCommandReview, FeishuCommandSupportApproximation, "Claude `/review` 当前不纳入 visible MVP；在 detached review contract 补齐前保持隐藏并拒绝直接执行。"),
		commandSupportHiddenReject(FeishuCommandPatch, FeishuCommandSupportApproximation, "Claude `/bendtomywill` 当前不纳入 visible MVP；在 turn patch contract 补齐前保持隐藏并拒绝直接执行。"),
		commandSupportHiddenAllowed(FeishuCommandUseAll),
		commandSupportVisibleAs(FeishuCommandSteerAll, FeishuCommandSupportApproximation, "Claude 当前支持把文本与本地图片补充并入当前轮；远程图片与 document 输入仍需等待本轮结束或改走新消息。"),
		commandSupportHiddenReject(FeishuCommandPlan, FeishuCommandSupportReject, "Claude 计划确认走 request bridge；在显式 plan contract 落地前不开放 `/plan` 命令入口。"),
		commandSupportHiddenReject(FeishuCommandAutoWhip, FeishuCommandSupportReject, claudeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandAutoContinue, FeishuCommandSupportReject, claudeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandCodexProvider, FeishuCommandSupportReject, "当前不在 Codex 模式，暂时不能切换 Codex Provider。请先 `/mode codex`。"),
		commandSupportHiddenReject(FeishuCommandFollow, FeishuCommandSupportReject, claudeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandCron, FeishuCommandSupportReject, claudeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandVSCodeMigrate, FeishuCommandSupportReject, claudeDefaultRejectNote),
	),
	"vscode": newFeishuCommandDisplayProfile("vscode", true,
		commandSupportVisible(FeishuCommandStop),
		commandSupportVisible(FeishuCommandCompact),
		commandSupportVisible(FeishuCommandSteerAll),
		commandSupportVisible(FeishuCommandStatus),
		commandSupportVisible(FeishuCommandReasoning),
		commandSupportVisible(FeishuCommandModel),
		commandSupportVisible(FeishuCommandAccess),
		commandSupportVisible(FeishuCommandPlan),
		commandSupportVisible(FeishuCommandVerbose),
		commandSupportVisible(FeishuCommandAutoContinue),
		commandSupportVisible(FeishuCommandList),
		commandSupportVisible(FeishuCommandUse),
		commandSupportVisible(FeishuCommandUseAll),
		commandSupportVisible(FeishuCommandDetach),
		commandSupportVisibleWithStages(FeishuCommandFollow, FeishuCommandMenuStageVSCodeWorking),
		commandSupportVisible(FeishuCommandAutoWhip),
		commandSupportVisible(FeishuCommandHistory),
		commandSupportVisible(FeishuCommandCron),
		commandSupportVisible(FeishuCommandSendFile),
		commandSupportVisible(FeishuCommandMode),
		commandSupportVisible(FeishuCommandUpgrade),
		commandSupportVisible(FeishuCommandDebug),
		commandSupportVisible(FeishuCommandHelp),
		commandSupportVisible(FeishuCommandMenu),
		commandSupportVisible(FeishuCommandVSCodeMigrate),
		commandSupportHiddenReject(FeishuCommandClaudeProfile, FeishuCommandSupportReject, "当前不在 Claude 模式，暂时不能切换 Claude 配置。请先 `/mode claude`。"),
	),
}

const (
	claudeDefaultRejectNote = "当前 Claude 模式暂不支持这个命令。"
)

func ResolveFeishuCommandDisplayProfileForContext(ctx CatalogContext) FeishuCommandDisplayProfile {
	normalized := NormalizeCatalogContext(ctx)
	profile, ok := feishuCommandDisplayProfiles[VisibleModeForCatalogContext(normalized)]
	if !ok {
		profile = feishuCommandDisplayProfiles["codex"]
	}
	return profile
}

func (p FeishuCommandDisplayProfile) FamilyProfile(familyID string) (FeishuCommandDisplayFamilyProfile, bool) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return FeishuCommandDisplayFamilyProfile{}, false
	}
	profile, ok := p.Families[familyID]
	if !ok || !profile.Visible {
		return FeishuCommandDisplayFamilyProfile{}, false
	}
	return profile, true
}

func (p FeishuCommandDisplayProfile) IncludesFamily(familyID string) bool {
	_, ok := p.FamilyProfile(familyID)
	return ok
}

func (p FeishuCommandDisplayProfile) MenuVisibleInStage(familyID, stage string) bool {
	profile, ok := p.FamilyProfile(familyID)
	if !ok {
		return false
	}
	return profile.MenuVisibleInStage(stage)
}

func (p FeishuCommandDisplayProfile) VisibleFamiliesForGroup(groupID string) []string {
	defs := FeishuCommandDefinitionsForGroup(groupID)
	visible := make([]string, 0, len(defs))
	for _, def := range defs {
		if p.IncludesFamily(def.ID) {
			visible = append(visible, def.ID)
		}
	}
	return visible
}

func (p FeishuCommandDisplayProfile) IncludesGroup(groupID string) bool {
	return len(p.VisibleFamiliesForGroup(groupID)) > 0
}

func ResolveFeishuCommandSupport(ctx CatalogContext, familyID string) (FeishuCommandSupport, bool) {
	ctx = NormalizeCatalogContext(ctx)
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return FeishuCommandSupport{}, false
	}
	if _, ok := FeishuCommandDefinitionByID(familyID); !ok {
		return FeishuCommandSupport{}, false
	}
	profile := ResolveFeishuCommandDisplayProfileForContext(ctx)
	family, ok := profile.Families[familyID]
	if !ok {
		family = FeishuCommandDisplayFamilyProfile{
			FamilyID:        familyID,
			DispatchAllowed: profile.DefaultDispatchAllowed,
			SupportKind:     profile.DefaultSupportKind,
			Note:            profile.DefaultRejectNote,
		}
	}
	kind := family.SupportKind
	if kind == "" {
		kind = profile.DefaultSupportKind
	}
	if kind == "" {
		kind = FeishuCommandSupportNative
	}
	note := strings.TrimSpace(family.Note)
	if note == "" && !family.DispatchAllowed {
		note = strings.TrimSpace(profile.DefaultRejectNote)
	}
	return FeishuCommandSupport{
		FamilyID:        familyID,
		Backend:         ctx.Backend,
		Kind:            kind,
		Visible:         family.Visible,
		DispatchAllowed: family.DispatchAllowed,
		Note:            note,
	}, true
}

func ResolveFeishuActionSupport(ctx CatalogContext, action Action) (FeishuCommandSupport, bool) {
	ctx = NormalizeCatalogContext(ctx)
	if familyID := strings.TrimSpace(action.CatalogFamilyID); familyID != "" {
		return ResolveFeishuCommandSupport(ctx, familyID)
	}
	if resolved, ok := ResolveFeishuActionCatalog(ctx, action); ok {
		return ResolveFeishuCommandSupport(ctx, resolved.FamilyID)
	}
	return FeishuCommandSupport{}, false
}

func (f FeishuCommandDisplayFamilyProfile) MenuVisibleInStage(stage string) bool {
	if len(f.MenuStages) == 0 {
		return true
	}
	_, ok := f.MenuStages[NormalizeFeishuCommandMenuStage(stage)]
	return ok
}

func commandSupportVisible(familyID string) FeishuCommandDisplayFamilyProfile {
	return commandSupportVisibleWithStages(familyID)
}

func commandSupportVisibleAs(familyID string, kind FeishuCommandSupportKind, note string) FeishuCommandDisplayFamilyProfile {
	family := commandSupportVisible(familyID)
	family.SupportKind = kind
	family.Note = strings.TrimSpace(note)
	return family
}

func commandSupportVisibleWithStages(familyID string, stages ...FeishuCommandMenuStage) FeishuCommandDisplayFamilyProfile {
	familyID = strings.TrimSpace(familyID)
	profile := FeishuCommandDisplayFamilyProfile{
		FamilyID:        familyID,
		Visible:         true,
		DispatchAllowed: true,
		SupportKind:     FeishuCommandSupportNative,
	}
	if len(stages) == 0 {
		return profile
	}
	profile.MenuStages = make(map[FeishuCommandMenuStage]struct{}, len(stages))
	for _, stage := range stages {
		normalized := NormalizeFeishuCommandMenuStage(string(stage))
		profile.MenuStages[normalized] = struct{}{}
	}
	return profile
}

func commandSupportHiddenAllowed(familyID string) FeishuCommandDisplayFamilyProfile {
	return FeishuCommandDisplayFamilyProfile{
		FamilyID:        strings.TrimSpace(familyID),
		DispatchAllowed: true,
		SupportKind:     FeishuCommandSupportNative,
	}
}

func commandSupportHiddenReject(familyID string, kind FeishuCommandSupportKind, note string) FeishuCommandDisplayFamilyProfile {
	if kind == "" {
		kind = FeishuCommandSupportReject
	}
	return FeishuCommandDisplayFamilyProfile{
		FamilyID:        strings.TrimSpace(familyID),
		DispatchAllowed: false,
		SupportKind:     kind,
		Note:            strings.TrimSpace(note),
	}
}

func newFeishuCommandDisplayProfile(visibleMode string, defaultDispatchAllowed bool, families ...FeishuCommandDisplayFamilyProfile) FeishuCommandDisplayProfile {
	profile := FeishuCommandDisplayProfile{
		VisibleMode:            strings.TrimSpace(strings.ToLower(visibleMode)),
		DefaultDispatchAllowed: defaultDispatchAllowed,
		DefaultSupportKind:     FeishuCommandSupportNative,
		DefaultRejectNote:      "当前模式暂不支持这个命令。",
		Families:               make(map[string]FeishuCommandDisplayFamilyProfile, len(families)),
	}
	if !defaultDispatchAllowed {
		profile.DefaultSupportKind = FeishuCommandSupportReject
		profile.DefaultRejectNote = claudeDefaultRejectNote
	}
	for _, family := range families {
		familyID := strings.TrimSpace(family.FamilyID)
		if familyID == "" {
			continue
		}
		profile.Families[familyID] = family
	}
	return profile
}

func (p FeishuCommandDisplayProfile) withAdditionalFamilies(families ...FeishuCommandDisplayFamilyProfile) FeishuCommandDisplayProfile {
	if len(families) == 0 {
		return p
	}
	cloned := FeishuCommandDisplayProfile{
		VisibleMode:            p.VisibleMode,
		DefaultDispatchAllowed: p.DefaultDispatchAllowed,
		DefaultSupportKind:     p.DefaultSupportKind,
		DefaultRejectNote:      p.DefaultRejectNote,
		Families:               make(map[string]FeishuCommandDisplayFamilyProfile, len(p.Families)+len(families)),
	}
	for familyID, profile := range p.Families {
		cloned.Families[familyID] = profile
	}
	for _, family := range families {
		familyID := strings.TrimSpace(family.FamilyID)
		if familyID == "" {
			continue
		}
		cloned.Families[familyID] = family
	}
	return cloned
}
