package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const modelCatalogMenuOptionLimit = 50

const modelReasoningAutoOptionValue = "clear"

type modelReasoningSupport int

const (
	modelReasoningUnknown modelReasoningSupport = iota
	modelReasoningSupported
	modelReasoningUnsupported
)

type modelReasoningValidation struct {
	Support          modelReasoningSupport
	Model            string
	Effort           string
	SupportedEfforts []string
	DefaultEffort    string
	Detail           string
}

func (s *Service) applyModelCatalogUpdated(inst *state.InstanceRecord, event agentproto.Event) {
	if inst == nil || event.ModelCatalog == nil {
		return
	}
	incoming := agentproto.CloneModelCatalogSnapshot(event.ModelCatalog)
	if incoming.RefreshedAt.IsZero() {
		incoming.RefreshedAt = s.now().UTC()
	}
	if incoming.ErrorMessage != "" || incoming.Unsupported {
		if inst.ModelCatalog == nil || len(inst.ModelCatalog.Entries) == 0 {
			inst.ModelCatalog = incoming
			return
		}
		preserved := agentproto.CloneModelCatalogSnapshot(inst.ModelCatalog)
		preserved.ErrorMessage = incoming.ErrorMessage
		preserved.Unsupported = incoming.Unsupported
		preserved.RefreshedAt = incoming.RefreshedAt
		inst.ModelCatalog = preserved
		return
	}
	inst.ModelCatalog = incoming
}

func (s *Service) modelCatalogCommandOptions(inst *state.InstanceRecord) ([]control.CommandCatalogFormFieldOption, bool) {
	if inst == nil || inst.ModelCatalog == nil || len(inst.ModelCatalog.Entries) == 0 {
		return nil, false
	}
	entries := make([]agentproto.ModelCatalogEntry, 0, len(inst.ModelCatalog.Entries))
	labelCounts := map[string]int{}
	for _, entry := range inst.ModelCatalog.Entries {
		model := strings.TrimSpace(entry.Model)
		if model == "" || entry.Hidden {
			continue
		}
		entry.Model = model
		entry.DisplayName = strings.TrimSpace(entry.DisplayName)
		entries = append(entries, entry)
		labelCounts[modelCatalogOptionBaseLabel(entry)]++
	}
	options := make([]control.CommandCatalogFormFieldOption, 0, len(entries))
	for _, entry := range entries {
		if len(options) >= modelCatalogMenuOptionLimit {
			break
		}
		label := modelCatalogOptionBaseLabel(entry)
		if labelCounts[label] > 1 && !strings.EqualFold(label, entry.Model) {
			label = fmt.Sprintf("%s（%s）", label, entry.Model)
		}
		options = append(options, control.CommandCatalogFormFieldOption{
			Label: label,
			Value: entry.Model,
		})
	}
	return options, len(entries) > modelCatalogMenuOptionLimit
}

func (s *Service) modelReasoningCommandOptions(inst *state.InstanceRecord, model string) ([]control.CommandCatalogFormFieldOption, string, string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return modelReasoningAutomaticOptions(), "info", "当前模型尚未明确；卡片只提供自动，若确定底层支持，可手动发送 /reasoning <effort>。"
	}
	if modelReasoningCatalogUnavailable(inst) {
		return modelReasoningAutomaticOptions(), "info", modelReasoningLookupStatusText(inst, model)
	}
	entry, found := modelCatalogEntryForModel(inst, model)
	if !found {
		return modelReasoningAutomaticOptions(), "info", modelReasoningLookupStatusText(inst, model)
	}
	efforts := modelReasoningSupportedEfforts(entry)
	if len(efforts) == 0 {
		return modelReasoningAutomaticOptions(), "info", "当前模型没有声明可用推理强度；卡片只提供自动，若确定底层支持，可手动发送 /reasoning <effort>。"
	}
	options := make([]control.CommandCatalogFormFieldOption, 0, len(efforts)+1)
	options = append(options, modelReasoningAutomaticOptions()...)
	for _, effort := range efforts {
		options = append(options, control.CommandCatalogFormFieldOption{
			Label: effort,
			Value: effort,
		})
	}
	if defaultEffort := normalizeModelReasoningEffort(entry.DefaultReasoningEffort); defaultEffort != "" {
		return options, "info", "当前模型默认推理强度由 Codex 决定；不设置覆盖时将保持自动。"
	}
	return options, "", ""
}

func modelReasoningAutomaticOptions() []control.CommandCatalogFormFieldOption {
	return []control.CommandCatalogFormFieldOption{{
		Label: "自动",
		Value: modelReasoningAutoOptionValue,
	}}
}

func (s *Service) validateReasoningEffortForPromptModel(inst *state.InstanceRecord, backend agentproto.Backend, model, effort string) modelReasoningValidation {
	effort = normalizeModelReasoningEffort(effort)
	model = strings.TrimSpace(model)
	if effort == "" {
		return modelReasoningValidation{
			Support: modelReasoningUnsupported,
			Model:   model,
			Detail:  "推理强度不能为空。",
		}
	}
	if isClearCommand(effort) {
		return modelReasoningValidation{
			Support: modelReasoningUnsupported,
			Model:   model,
			Effort:  effort,
			Detail:  "如需清除推理强度，请使用 /reasoning clear；如需清除模型和推理强度，请使用 /model clear。",
		}
	}
	if agentproto.NormalizeBackend(backend) == agentproto.BackendClaude {
		normalized, ok := control.NormalizeReasoningEffortForBackend(backend, effort)
		if !ok {
			return modelReasoningValidation{
				Support: modelReasoningUnsupported,
				Model:   model,
				Effort:  effort,
				Detail:  "推理强度建议使用 " + control.ReasoningEffortHintForBackend(backend) + "。",
			}
		}
		return modelReasoningValidation{
			Support: modelReasoningUnknown,
			Model:   model,
			Effort:  normalized,
		}
	}
	validation := s.checkModelReasoningSupport(inst, model, effort)
	if validation.Support != modelReasoningUnknown {
		return validation
	}
	validation.Effort = effort
	return validation
}

func (s *Service) checkModelReasoningSupport(inst *state.InstanceRecord, model, effort string) modelReasoningValidation {
	model = strings.TrimSpace(model)
	effort = normalizeModelReasoningEffort(effort)
	validation := modelReasoningValidation{
		Support: modelReasoningUnknown,
		Model:   model,
		Effort:  effort,
	}
	if model == "" || effort == "" {
		validation.Detail = "当前模型或推理强度为空，无法本地校验。"
		return validation
	}
	if modelReasoningCatalogUnavailable(inst) {
		validation.Detail = modelReasoningLookupStatusText(inst, model)
		return validation
	}
	entry, found := modelCatalogEntryForModel(inst, model)
	if !found {
		validation.Detail = modelReasoningLookupStatusText(inst, model)
		return validation
	}
	validation.DefaultEffort = normalizeModelReasoningEffort(entry.DefaultReasoningEffort)
	validation.SupportedEfforts = modelReasoningSupportedEfforts(entry)
	if len(validation.SupportedEfforts) == 0 {
		validation.Detail = "当前模型没有声明可用推理强度，无法本地校验。"
		return validation
	}
	for _, supported := range validation.SupportedEfforts {
		if strings.EqualFold(supported, effort) {
			validation.Support = modelReasoningSupported
			validation.Effort = supported
			return validation
		}
	}
	validation.Support = modelReasoningUnsupported
	validation.Detail = "当前模型不支持这个推理强度。"
	return validation
}

func modelCatalogEntryForModel(inst *state.InstanceRecord, model string) (agentproto.ModelCatalogEntry, bool) {
	model = strings.TrimSpace(model)
	if model == "" || inst == nil || inst.ModelCatalog == nil || len(inst.ModelCatalog.Entries) == 0 {
		return agentproto.ModelCatalogEntry{}, false
	}
	for _, entry := range inst.ModelCatalog.Entries {
		entryModel := strings.TrimSpace(entry.Model)
		entryID := strings.TrimSpace(entry.ID)
		if strings.EqualFold(entryModel, model) || (entryID != "" && strings.EqualFold(entryID, model)) {
			entry.Model = entryModel
			entry.ID = entryID
			return agentproto.CloneModelCatalogEntry(entry), true
		}
	}
	return agentproto.ModelCatalogEntry{}, false
}

func modelReasoningSupportedEfforts(entry agentproto.ModelCatalogEntry) []string {
	seen := map[string]bool{}
	efforts := make([]string, 0, len(entry.SupportedReasoningEfforts))
	for _, option := range entry.SupportedReasoningEfforts {
		effort := normalizeModelReasoningEffort(option.ReasoningEffort)
		if effort == "" || seen[effort] {
			continue
		}
		seen[effort] = true
		efforts = append(efforts, effort)
	}
	return efforts
}

func normalizeModelReasoningEffort(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func modelReasoningCatalogUnavailable(inst *state.InstanceRecord) bool {
	return inst == nil ||
		inst.ModelCatalog == nil ||
		inst.ModelCatalog.Unsupported ||
		strings.TrimSpace(inst.ModelCatalog.ErrorMessage) != "" ||
		len(inst.ModelCatalog.Entries) == 0
}

func modelReasoningLookupStatusText(inst *state.InstanceRecord, model string) string {
	if inst == nil {
		return "当前还没有接管 Codex 实例；卡片只提供自动，若确定底层支持，可手动发送 /reasoning <effort>。"
	}
	if inst.ModelCatalog == nil {
		return "正在后台刷新可用模型列表；刷新完成前卡片只提供自动，若确定底层支持，可手动发送 /reasoning <effort>。"
	}
	if inst.ModelCatalog.Unsupported {
		return "当前 Codex 实例暂不支持动态模型列表；无法本地校验模型支持的推理强度。"
	}
	if strings.TrimSpace(inst.ModelCatalog.ErrorMessage) != "" {
		return "模型列表刷新失败；无法本地校验模型支持的推理强度。"
	}
	if len(inst.ModelCatalog.Entries) == 0 {
		return "当前模型列表为空；无法本地校验模型支持的推理强度。"
	}
	if strings.TrimSpace(model) == "" {
		return "当前模型尚未明确；无法本地校验模型支持的推理强度。"
	}
	if strings.TrimSpace(inst.ModelCatalog.NextCursor) != "" {
		return "当前模型不在已加载的模型列表片段中；无法本地校验模型支持的推理强度。"
	}
	return "当前模型不在可用模型列表中；无法本地校验模型支持的推理强度。"
}

func modelCatalogOptionBaseLabel(entry agentproto.ModelCatalogEntry) string {
	label := strings.TrimSpace(entry.DisplayName)
	if label == "" {
		label = strings.TrimSpace(entry.Model)
	}
	return label
}

func (s *Service) maybeModelCatalogStatusText(inst *state.InstanceRecord, truncated bool) (string, string) {
	if truncated {
		return "info", "模型列表较长，当前只显示前 50 个；没有出现在下拉里的模型仍可手动输入完整名称。"
	}
	if inst == nil {
		return "info", "当前还没有接管 Codex 实例，暂时只能手动输入模型名。"
	}
	if inst.ModelCatalog == nil {
		return "info", "正在后台刷新可用模型列表；刷新完成前可手动输入模型名。"
	}
	if inst.ModelCatalog.Unsupported {
		return "info", "当前 Codex 实例暂不支持动态模型列表；请手动输入模型名。"
	}
	if strings.TrimSpace(inst.ModelCatalog.ErrorMessage) != "" && len(inst.ModelCatalog.Entries) == 0 {
		return "info", "模型列表刷新失败；请手动输入模型名。"
	}
	if len(inst.ModelCatalog.Entries) == 0 {
		return "info", "当前模型列表为空；请手动输入模型名。"
	}
	return "", ""
}

func (s *Service) modelCatalogRefreshEvents(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if !state.InstanceSupportsModelCatalog(inst) {
		return nil
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandModelList,
			ModelList: agentproto.ModelListCommand{
				IncludeHidden: false,
				Limit:         modelCatalogMenuOptionLimit,
			},
		},
	}}
}

func NonTurnAgentCommand(kind agentproto.CommandKind) bool {
	switch kind {
	case agentproto.CommandModelList:
		return true
	default:
		return false
	}
}
