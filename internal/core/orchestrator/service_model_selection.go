package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// ModelCatalogEntryForModel shares the legacy picker matching/canonicalization
// without constructing an Instance or borrowing a Surface's authority.
func ModelCatalogEntryForModel(catalog *agentproto.ModelCatalogSnapshot, model string) (agentproto.ModelCatalogEntry, bool) {
	model = strings.TrimSpace(model)
	if model == "" || catalog == nil {
		return agentproto.ModelCatalogEntry{}, false
	}
	for _, entry := range catalog.Entries {
		candidate := strings.TrimSpace(entry.Model)
		id := strings.TrimSpace(entry.ID)
		if strings.EqualFold(candidate, model) || (id != "" && strings.EqualFold(id, model)) {
			entry.Model = candidate
			entry.ID = id
			return agentproto.CloneModelCatalogEntry(entry), true
		}
	}
	return agentproto.ModelCatalogEntry{}, false
}
func FixedCodexProfileSelection(profile state.CodexProfileSummary) (model, reasoning string, fixed bool) {
	model, fixed = fixedCodexAPIProfileModel(profile)
	if fixed {
		reasoning = fixedCodexAPIProfileReasoning(profile)
	}
	return
}

// ValidateCatalogSelection rejects unlisted models instead of treating absent or
// stale catalog data as a positive model-validation result. Backend normalization
// and advertised reasoning variants reuse the existing command helpers.
func ValidateCatalogSelection(backend agentproto.Backend, catalog *agentproto.ModelCatalogSnapshot, model, reasoning string) (string, string, error) {
	if catalog == nil || catalog.Unsupported || catalog.ErrorMessage != "" || len(catalog.Entries) == 0 {
		return "", "", fmt.Errorf("当前没有此配置的可用模型列表；请刷新列表或使用已明确配置的固定 Profile")
	}
	entry, ok := ModelCatalogEntryForModel(catalog, model)
	if !ok || entry.Hidden {
		return "", "", fmt.Errorf("所选模型不在当前已加载的可用列表中")
	}
	model = entry.Model
	if model == "" {
		model = entry.ID
	}
	if reasoning == "" {
		return model, "", nil
	}
	normalized, ok := control.NormalizeReasoningEffortForBackend(backend, reasoning)
	if !ok {
		return "", "", fmt.Errorf("推理强度无效：%s", control.ReasoningEffortHintForBackend(backend))
	}
	efforts := modelReasoningSupportedEfforts(entry)
	if len(efforts) == 0 {
		if backend == agentproto.BackendClaude {
			return model, normalized, nil
		}
		return "", "", fmt.Errorf("当前模型尚未声明可用推理强度，请保持自动或刷新配置列表")
	}
	for _, effort := range efforts {
		if strings.EqualFold(effort, normalized) {
			return model, effort, nil
		}
	}
	return "", "", fmt.Errorf("当前模型不支持所选推理强度")
}
