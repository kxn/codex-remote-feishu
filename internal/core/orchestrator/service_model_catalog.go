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
