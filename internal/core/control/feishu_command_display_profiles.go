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
		commandSupportVisible(FeishuCommandCodexProfile),
		commandSupportVisible(FeishuCommandAutoContinue),
		commandSupportVisible(FeishuCommandWorkspace),
		commandSupportVisible(FeishuCommandWorkspaceList),
		commandSupportVisible(FeishuCommandWorkspaceNew),
		commandSupportVisible(FeishuCommandWorkspaceNewDir),
		commandSupportVisible(FeishuCommandWorkspaceNewGit),
		commandSupportVisible(FeishuCommandWorkspaceNewWorktree),
		commandSupportVisible(FeishuCommandWorkspaceDetach),
		commandSupportVisible(FeishuCommandAutoWhip),
		commandSupportVisible(FeishuCommandPrimary),
		commandSupportVisible(FeishuCommandCoworkers),
		commandSupportVisible(FeishuCommandHistory),
		commandSupportVisible(FeishuCommandReview),
		commandSupportVisible(FeishuCommandCron),
		commandSupportVisible(FeishuCommandSendFile),
		commandSupportVisible(FeishuCommandMode),
		commandSupportVisible(FeishuCommandAdmin),
		commandSupportHiddenAllowed(FeishuCommandAdminSubcommand),
		commandSupportVisible(FeishuCommandUpgrade),
		commandSupportVisibleWithStages(FeishuCommandPatch, FeishuCommandMenuStageNormalWorking),
		commandSupportVisible(FeishuCommandDebug),
		commandSupportVisible(FeishuCommandHelp),
		commandSupportVisible(FeishuCommandMenu),
		commandSupportHiddenReject(FeishuCommandClaudeProfile, FeishuCommandSupportReject, "当前不在 Claude 模式，暂时不能切换 Claude 配置。请先 `/mode claude`。"),
		commandSupportHiddenReject(FeishuCommandOpenCodeProfile, FeishuCommandSupportReject, "当前不在 OpenCode 模式，暂时不能切换 OpenCode Profile。请先 `/mode opencode`。"),
	),
	"claude": newFeishuCommandDisplayProfile("claude", false,
		commandSupportVisible(FeishuCommandStop),
		commandSupportVisibleAs(FeishuCommandNew, FeishuCommandSupportApproximation, "Claude 会话切换沿用现有产品壳，但底层走 backend-aware session catalog 与 route contract。"),
		commandSupportVisible(FeishuCommandStatus),
		commandSupportVisible(FeishuCommandReasoning),
		commandSupportHiddenReject(FeishuCommandModel, FeishuCommandSupportReject, "Claude 模型请在 Claude 配置里设置，当前飞书会话不支持临时切换模型。"),
		commandSupportVisible(FeishuCommandAccess),
		commandSupportVisible(FeishuCommandWorkspace),
		commandSupportVisibleAs(FeishuCommandWorkspaceList, FeishuCommandSupportApproximation, "Claude 会话切换沿用现有产品壳，但底层走 backend-aware session catalog 与 route contract。"),
		commandSupportVisible(FeishuCommandWorkspaceNew),
		commandSupportVisible(FeishuCommandWorkspaceNewDir),
		commandSupportVisible(FeishuCommandWorkspaceNewGit),
		commandSupportVisible(FeishuCommandWorkspaceNewWorktree),
		commandSupportVisible(FeishuCommandWorkspaceDetach),
		commandSupportVisible(FeishuCommandVerbose),
		commandSupportVisible(FeishuCommandHistory),
		commandSupportVisible(FeishuCommandPrimary),
		commandSupportVisible(FeishuCommandCoworkers),
		commandSupportVisible(FeishuCommandSendFile),
		commandSupportVisible(FeishuCommandMode),
		commandSupportVisible(FeishuCommandAdmin),
		commandSupportHiddenAllowed(FeishuCommandAdminSubcommand),
		commandSupportVisible(FeishuCommandClaudeProfile),
		commandSupportVisible(FeishuCommandUpgrade),
		commandSupportVisible(FeishuCommandDebug),
		commandSupportVisible(FeishuCommandHelp),
		commandSupportVisible(FeishuCommandMenu),
		commandSupportHiddenAllowed(FeishuCommandList),
		commandSupportHiddenAllowed(FeishuCommandUse),
		commandSupportHiddenAllowed(FeishuCommandDetach),
		commandSupportHiddenReject(FeishuCommandCompact, FeishuCommandSupportPassthrough, "Claude `/compact` 目前只作为后续 passthrough 候选；在 runtime host 收口前保持隐藏并拒绝直接执行。"),
		commandSupportHiddenReject(FeishuCommandReview, FeishuCommandSupportApproximation, "Claude `/review` 当前不纳入 visible MVP；在 detached review contract 补齐前保持隐藏并拒绝直接执行。"),
		commandSupportHiddenReject(FeishuCommandPatch, FeishuCommandSupportApproximation, "Claude `/bendtomywill` 当前不纳入 visible MVP；在 turn patch contract 补齐前保持隐藏并拒绝直接执行。"),
		commandSupportHiddenAllowed(FeishuCommandUseAll),
		commandSupportVisibleAs(FeishuCommandSteerAll, FeishuCommandSupportApproximation, "Claude 当前支持把文本与本地图片补充并入当前轮；远程图片与 document 输入仍需等待本轮结束或改走新消息。"),
		commandSupportVisible(FeishuCommandPlan),
		commandSupportHiddenReject(FeishuCommandAutoWhip, FeishuCommandSupportReject, claudeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandAutoContinue, FeishuCommandSupportReject, claudeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandCodexProfile, FeishuCommandSupportReject, "当前不在 Codex 模式，暂时不能切换 Codex Profile。请先 `/mode codex`。"),
		commandSupportHiddenReject(FeishuCommandOpenCodeProfile, FeishuCommandSupportReject, "当前不在 OpenCode 模式，暂时不能切换 OpenCode Profile。请先 `/mode opencode`。"),
		commandSupportHiddenReject(FeishuCommandFollow, FeishuCommandSupportReject, claudeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandCron, FeishuCommandSupportReject, claudeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandVSCodeMigrate, FeishuCommandSupportReject, claudeDefaultRejectNote),
	),
	"opencode": newOpenCodeFeishuCommandDisplayProfile(),
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
		commandSupportVisible(FeishuCommandPrimary),
		commandSupportVisible(FeishuCommandCoworkers),
		commandSupportVisible(FeishuCommandHistory),
		commandSupportVisible(FeishuCommandCron),
		commandSupportVisible(FeishuCommandSendFile),
		commandSupportVisible(FeishuCommandMode),
		commandSupportVisible(FeishuCommandAdmin),
		commandSupportHiddenAllowed(FeishuCommandAdminSubcommand),
		commandSupportVisible(FeishuCommandUpgrade),
		commandSupportVisible(FeishuCommandDebug),
		commandSupportVisible(FeishuCommandHelp),
		commandSupportVisible(FeishuCommandMenu),
		commandSupportVisible(FeishuCommandVSCodeMigrate),
		commandSupportHiddenReject(FeishuCommandClaudeProfile, FeishuCommandSupportReject, "当前不在 Claude 模式，暂时不能切换 Claude 配置。请先 `/mode claude`。"),
		commandSupportHiddenReject(FeishuCommandOpenCodeProfile, FeishuCommandSupportReject, "当前不在 OpenCode 模式，暂时不能切换 OpenCode Profile。请先 `/mode opencode`。"),
	),
}

const (
	claudeDefaultRejectNote   = "当前 Claude 模式暂不支持这个命令。"
	opencodeApproxNote        = "OpenCode 当前按现有会话控制近似处理。"
	opencodeDefaultRejectNote = "当前 OpenCode 模式暂不支持这个命令。"
)

func newOpenCodeFeishuCommandDisplayProfile() FeishuCommandDisplayProfile {
	profile := newFeishuCommandDisplayProfile("opencode", false,
		commandSupportVisible(FeishuCommandStop),
		commandSupportHiddenReject(FeishuCommandCompact, FeishuCommandSupportReject, opencodeDefaultRejectNote),
		commandSupportVisibleAs(FeishuCommandSteerAll, FeishuCommandSupportApproximation, opencodeApproxNote),
		commandSupportVisibleAs(FeishuCommandNew, FeishuCommandSupportApproximation, opencodeApproxNote),
		commandSupportVisible(FeishuCommandStatus),
		commandSupportHiddenReject(FeishuCommandModel, FeishuCommandSupportReject, "OpenCode 模型请在 OpenCode profile 或原生配置里设置，当前飞书会话不支持临时切换模型。"),
		commandSupportHiddenReject(FeishuCommandReasoning, FeishuCommandSupportReject, "OpenCode 推理强度请在 OpenCode profile 或原生配置里设置，当前飞书会话不支持临时切换。"),
		commandSupportVisible(FeishuCommandAccess),
		commandSupportHiddenReject(FeishuCommandPlan, FeishuCommandSupportReject, "OpenCode Plan mode 由 OpenCode runtime 自身决定，当前飞书会话不支持临时切换。"),
		commandSupportVisible(FeishuCommandVerbose),
		commandSupportVisible(FeishuCommandOpenCodeProfile),
		commandSupportVisible(FeishuCommandWorkspace),
		commandSupportVisible(FeishuCommandWorkspaceList),
		commandSupportVisible(FeishuCommandWorkspaceNew),
		commandSupportVisible(FeishuCommandWorkspaceNewDir),
		commandSupportVisible(FeishuCommandWorkspaceNewGit),
		commandSupportVisible(FeishuCommandWorkspaceNewWorktree),
		commandSupportVisible(FeishuCommandWorkspaceDetach),
		commandSupportVisible(FeishuCommandPrimary),
		commandSupportVisible(FeishuCommandCoworkers),
		commandSupportVisible(FeishuCommandHistory),
		commandSupportHiddenReject(FeishuCommandReview, FeishuCommandSupportReject, opencodeDefaultRejectNote),
		commandSupportVisible(FeishuCommandSendFile),
		commandSupportVisible(FeishuCommandMode),
		commandSupportVisible(FeishuCommandAdmin),
		commandSupportHiddenAllowed(FeishuCommandAdminSubcommand),
		commandSupportVisible(FeishuCommandUpgrade),
		commandSupportVisible(FeishuCommandDebug),
		commandSupportVisible(FeishuCommandHelp),
		commandSupportVisible(FeishuCommandMenu),
		commandSupportHiddenAllowed(FeishuCommandList),
		commandSupportHiddenAllowed(FeishuCommandUse),
		commandSupportHiddenAllowed(FeishuCommandDetach),
		commandSupportHiddenAllowed(FeishuCommandUseAll),
		commandSupportHiddenReject(FeishuCommandPatch, FeishuCommandSupportReject, opencodeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandAutoWhip, FeishuCommandSupportReject, opencodeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandAutoContinue, FeishuCommandSupportReject, opencodeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandCodexProfile, FeishuCommandSupportReject, "当前不在 Codex 模式，暂时不能切换 Codex Profile。请先 `/mode codex`。"),
		commandSupportHiddenReject(FeishuCommandClaudeProfile, FeishuCommandSupportReject, "当前不在 Claude 模式，暂时不能切换 Claude 配置。请先 `/mode claude`。"),
		commandSupportHiddenReject(FeishuCommandFollow, FeishuCommandSupportReject, opencodeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandCron, FeishuCommandSupportReject, opencodeDefaultRejectNote),
		commandSupportHiddenReject(FeishuCommandVSCodeMigrate, FeishuCommandSupportReject, opencodeDefaultRejectNote),
	)
	profile.DefaultRejectNote = opencodeDefaultRejectNote
	return profile
}

func ResolveFeishuCommandDisplayProfileForContext(ctx CatalogContext) FeishuCommandDisplayProfile {
	normalized := NormalizeCatalogContext(ctx)
	profile, ok := feishuCommandDisplayProfiles[VisibleModeForCatalogContext(normalized)]
	if !ok {
		profile = feishuCommandDisplayProfiles["codex"]
	}
	if normalized.BotCapabilitySettingsReadOnly {
		profile = profile.withAdditionalFamilies(botCapabilitySettingsReadOnlyFamilies()...)
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

func botCapabilitySettingsReadOnlyFamilies() []FeishuCommandDisplayFamilyProfile {
	const note = "此设置请在和机器人的私聊中修改。"
	return []FeishuCommandDisplayFamilyProfile{
		commandSupportHiddenReject(FeishuCommandMode, FeishuCommandSupportReject, note),
		commandSupportHiddenReject(FeishuCommandCodexProfile, FeishuCommandSupportReject, note),
		commandSupportHiddenReject(FeishuCommandClaudeProfile, FeishuCommandSupportReject, note),
		commandSupportHiddenReject(FeishuCommandModel, FeishuCommandSupportReject, note),
		commandSupportHiddenReject(FeishuCommandReasoning, FeishuCommandSupportReject, note),
		commandSupportHiddenReject(FeishuCommandAccess, FeishuCommandSupportReject, note),
		commandSupportHiddenReject(FeishuCommandPlan, FeishuCommandSupportReject, note),
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
