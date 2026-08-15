package orchestrator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) MaterializeOpenCodeProfiles(records []state.OpenCodeProfileSummary) {
	if s.root == nil {
		return
	}
	s.root.OpenCodeProfiles = map[string]state.OpenCodeProfileSummary{}
	defaultRecord := normalizeOpenCodeProfileSummary(state.OpenCodeProfileSummary{
		ID:        state.DefaultOpenCodeProfileID,
		Revision:  1,
		Name:      "本机默认",
		BuiltIn:   true,
		Available: true,
	})
	s.root.OpenCodeProfiles[defaultRecord.ID] = defaultRecord
	for _, record := range records {
		current := normalizeOpenCodeProfileSummary(record)
		if strings.TrimSpace(current.ID) == "" {
			continue
		}
		s.root.OpenCodeProfiles[current.ID] = current
	}
}

func (s *Service) OpenCodeProfiles() []state.OpenCodeProfileSummary {
	if s.root == nil || len(s.root.OpenCodeProfiles) == 0 {
		return []state.OpenCodeProfileSummary{normalizeOpenCodeProfileSummary(state.OpenCodeProfileSummary{
			ID:        state.DefaultOpenCodeProfileID,
			Revision:  1,
			Name:      "本机默认",
			BuiltIn:   true,
			Available: true,
		})}
	}
	profiles := make([]state.OpenCodeProfileSummary, 0, len(s.root.OpenCodeProfiles))
	for _, record := range s.root.OpenCodeProfiles {
		profiles = append(profiles, normalizeOpenCodeProfileSummary(record))
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		left := profiles[i]
		right := profiles[j]
		if left.BuiltIn != right.BuiltIn {
			return left.BuiltIn
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ID < right.ID
	})
	return profiles
}

func normalizeOpenCodeProfileSummary(value state.OpenCodeProfileSummary) state.OpenCodeProfileSummary {
	value.ID = state.NormalizeOpenCodeProfileID(value.ID)
	value.Name = strings.TrimSpace(value.Name)
	value.ProviderType = strings.TrimSpace(value.ProviderType)
	value.BaseURL = strings.TrimSpace(value.BaseURL)
	value.Model = strings.TrimSpace(value.Model)
	value.StatusCode = strings.TrimSpace(value.StatusCode)
	value.ETag = strings.TrimSpace(value.ETag)
	if value.ID == state.DefaultOpenCodeProfileID {
		value.Revision = maxUint64(value.Revision, 1)
		value.Name = "本机默认"
		value.BuiltIn = true
		value.Available = true
		value.Editable = false
		value.Deletable = false
	} else {
		if value.Name == "" {
			value.Name = value.ID
		}
		if value.Revision == 0 {
			value.Available = false
			if value.StatusCode == "" {
				value.StatusCode = "profile_revision_unavailable"
			}
		}
	}
	if value.ETag == "" && value.Revision != 0 {
		value.ETag = state.OpenCodeProfileDefinitionETag(value.ID, value.Revision)
	}
	return value
}

func maxUint64(left, right uint64) uint64 {
	if left > right {
		return left
	}
	return right
}

func (s *Service) resolveOpenCodeProfileSelection(value string) (state.OpenCodeProfileSummary, bool) {
	targetID := state.NormalizeOpenCodeProfileID(value)
	for _, profile := range s.OpenCodeProfiles() {
		if strings.EqualFold(strings.TrimSpace(profile.ID), targetID) {
			return profile, true
		}
	}
	return state.OpenCodeProfileSummary{}, false
}

func (s *Service) openCodeProfileCommandOptions() []control.CommandCatalogFormFieldOption {
	profiles := s.OpenCodeProfiles()
	if len(profiles) == 0 {
		return []control.CommandCatalogFormFieldOption{{
			Label: "本机默认",
			Value: state.DefaultOpenCodeProfileID,
		}}
	}
	labelCounts := map[string]int{}
	for _, profile := range profiles {
		label := strings.TrimSpace(profile.Name)
		if label == "" {
			label = profile.ID
		}
		labelCounts[label]++
	}
	options := make([]control.CommandCatalogFormFieldOption, 0, len(profiles))
	for _, profile := range profiles {
		label := strings.TrimSpace(profile.Name)
		if label == "" {
			label = profile.ID
		}
		if labelCounts[label] > 1 && !strings.EqualFold(label, strings.TrimSpace(profile.ID)) {
			label += "（" + strings.TrimSpace(profile.ID) + "）"
		}
		if !profile.Available {
			label += "（不可用）"
		}
		options = append(options, control.CommandCatalogFormFieldOption{
			Label: label,
			Value: strings.TrimSpace(profile.ID),
		})
	}
	return options
}

func (s *Service) handleOpenCodeProfileCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if !s.surfaceIsHeadless(surface) || s.surfaceBackend(surface) != agentproto.BackendOpenCode {
		text := "当前不在 OpenCode 模式，暂时不能切换 OpenCode Profile。请先 `/mode opencode`。"
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: text,
			})
		}
		return notice(surface, "opencode_profile_mode_required", text)
	}

	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return s.openConfigCommandPageForAction(surface, action)
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/opencodeprofile` 查看当前状态；`/opencodeprofile <profile-id>`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}

	target, ok := s.resolveOpenCodeProfileSelection(parts[1])
	if !ok {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "找不到这个 OpenCode Profile。请从下拉里选择已有 Profile，或到管理页先创建。",
			FormDefaultValue: state.NormalizeOpenCodeProfileID(parts[1]),
		})
	}
	if !target.Available {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       fmt.Sprintf("OpenCode Profile %s 当前不可用：%s", openCodeProfileDisplayName(target), openCodeProfileUnavailableReasonText(target)),
			FormDefaultValue: strings.TrimSpace(target.ID),
		})
	}

	currentProfileID := s.surfaceOpenCodeProfileID(surface)
	currentWorkspaceKey := normalizeWorkspaceClaimKey(s.surfaceCurrentWorkspaceKey(surface))
	targetLabel := openCodeProfileDisplayName(target)
	if target.ID == currentProfileID && openCodeAdmissionRefMatchesProfile(surface.OpenCodeAdmissionRef, target) {
		text := fmt.Sprintf("当前已在使用 OpenCode Profile：%s。", targetLabel)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "info",
				StatusText: text,
			})
		}
		return notice(surface, "opencode_profile_current", text)
	}

	if blocked := s.blockRouteMutation(surface); blocked != nil {
		if commandCardOwnsInlineResult(action) {
			text := ""
			if len(blocked) > 0 && blocked[0].Notice != nil {
				text = blocked[0].Notice.Text
			}
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: text,
			})
		}
		return blocked
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	if surface.PendingHeadless != nil || s.surfaceHasLiveRemoteWork(surface) || s.surfaceNeedsDelayedDetach(surface, inst) {
		text := "当前仍有执行中的 turn、派发中的请求、排队消息或工作区准备流程，暂时不能切换 OpenCode Profile。请等待完成、/stop，或先 /detach。"
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: text,
			})
		}
		return notice(surface, "opencode_profile_busy", text)
	}

	continuation := s.buildHeadlessContractSwitchContinuation(surface, currentWorkspaceKey, agentproto.BackendOpenCode)
	continuation = s.openCodeProfileSwitchNewThreadContinuation(surface, currentWorkspaceKey, continuation)
	events := s.discardDrafts(surface)
	events = s.queueHeadlessContractRestart(events, surface, continuation)
	events = append(events, s.finalizeDetachedSurface(surface)...)
	s.applyOpenCodeProfileSelection(surface, target, currentWorkspaceKey == "")
	reconcileEvents := s.reconcileGatewayHeadlessSurfacesAfterOpenCodeProfileChange(surface)
	if currentWorkspaceKey == "" {
		text := fmt.Sprintf("已切换到 OpenCode Profile：%s。当前没有接管中的工作区。", targetLabel)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: text,
			}, append(events, reconcileEvents...)...)
		}
		return append(append(events, reconcileEvents...), notice(surface, "opencode_profile_switched", text)...)
	}

	s.transitionSurfaceRouteCore(surface, nil, surfaceRouteCoreState{WorkspaceKey: currentWorkspaceKey})
	resumeEvents := s.restartHeadlessContractContinuation(surface, continuation)
	statusText := fmt.Sprintf("已切换到 OpenCode Profile：%s。正在重新准备当前工作区。", targetLabel)
	if commandCardOwnsInlineResult(action) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: statusText,
		}, append(append(events, reconcileEvents...), resumeEvents...)...)
	}
	events = append(append(events, reconcileEvents...), notice(surface, "opencode_profile_switched", statusText)...)
	return append(events, resumeEvents...)
}

func (s *Service) openCodeProfileSwitchNewThreadContinuation(surface *state.SurfaceConsoleRecord, workspaceKey string, previous headlessContractSwitchContinuation) headlessContractSwitchContinuation {
	next := s.buildHeadlessWorkspaceRouteRestartContinuation(surface, workspaceKey, agentproto.BackendOpenCode, true)
	next.RestartManagedNow = previous.RestartManagedNow
	next.RestartInstanceID = previous.RestartInstanceID
	return next
}

func (s *Service) applyOpenCodeProfileSelection(surface *state.SurfaceConsoleRecord, target state.OpenCodeProfileSummary, clearDetachedRuntime bool) {
	admissionRef := openCodeAdmissionRefForProfile(target)
	s.applySurfaceCapabilitySettingsMutation(surface, func(record *state.BotCapabilitySettingsRecord) {
		record.OpenCodeProfileID = target.ID
		record.OpenCodeAdmissionRef = state.NormalizeOpenCodeAdmissionRef(admissionRef)
	}, func(local *state.SurfaceConsoleRecord) {
		local.OpenCodeProfileID = state.NormalizeDesiredOpenCodeProfileID(target.ID)
		local.OpenCodeAdmissionRef = admissionRef
		if clearDetachedRuntime {
			// access/plan 为会话级设置，切换 profile 不重置。
			clearSurfacePromptRuntimeOverride(local)
		}
	})
	if state.SurfaceUsesBotCapabilitySettings(surface) {
		s.freezeOpenCodeAdmissionRefForGatewayProfile(surface, target.ID, admissionRef)
	}
}

func (s *Service) freezeOpenCodeAdmissionRefForGatewayProfile(current *state.SurfaceConsoleRecord, profileID string, admissionRef *state.OpenCodeAdmissionRef) {
	if s == nil || s.root == nil || current == nil {
		return
	}
	gatewayKey := state.BotCapabilitySettingsKey(current.GatewayID)
	if gatewayKey == "" {
		return
	}
	profileID = state.NormalizeOpenCodeProfileID(profileID)
	for _, surface := range s.root.Surfaces {
		if surface == nil || state.BotCapabilitySettingsKey(surface.GatewayID) != gatewayKey {
			continue
		}
		contract := s.surfaceDesiredContract(surface)
		if contract.Backend != agentproto.BackendOpenCode || state.EffectiveSurfaceOpenCodeProfileID(contract) != profileID {
			continue
		}
		surface.OpenCodeAdmissionRef = state.NormalizeOpenCodeAdmissionRef(admissionRef)
	}
}

func openCodeAdmissionRefForProfile(profile state.OpenCodeProfileSummary) *state.OpenCodeAdmissionRef {
	profile = normalizeOpenCodeProfileSummary(profile)
	if strings.TrimSpace(profile.ID) == "" || profile.Revision == 0 {
		return nil
	}
	return &state.OpenCodeAdmissionRef{
		ProfileRef: state.OpenCodeProfileRef{
			ID:       profile.ID,
			Revision: profile.Revision,
		},
	}
}

func openCodeAdmissionRefMatchesProfile(ref *state.OpenCodeAdmissionRef, profile state.OpenCodeProfileSummary) bool {
	expected := openCodeAdmissionRefForProfile(profile)
	actual := state.NormalizeOpenCodeAdmissionRef(ref)
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	return *actual == *expected
}

func openCodeProfileDisplayName(profile state.OpenCodeProfileSummary) string {
	if name := strings.TrimSpace(profile.Name); name != "" {
		return name
	}
	return strings.TrimSpace(profile.ID)
}

func openCodeProfileUnavailableReasonText(profile state.OpenCodeProfileSummary) string {
	switch strings.TrimSpace(profile.StatusCode) {
	case "profile_revision_unavailable":
		return "当前保存的 Profile 版本已经不可用，请到 Web 管理界面刷新或重新保存。"
	case "profile_definition_incomplete":
		return "配置不完整，请到 Web 管理界面补齐端点、模型和 API Key。"
	case "profile_secret_missing":
		return "缺少 API Key，请到 Web 管理界面补齐后再使用。"
	case "profile_catalog_degraded":
		return "Profile 目录暂不可用，请稍后刷新。"
	default:
		return "当前不可用，请到 Web 管理界面检查配置。"
	}
}
