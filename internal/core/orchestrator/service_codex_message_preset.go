package orchestrator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type codexMessagePreset struct {
	Key             string
	Model           string
	ReasoningEffort string
}

const codexMessagePresetUsage = "用法：在正文开头使用 [luna]、[terra]、[sol] 或 [astra]；可写成 [模型:low|medium|high|xhigh|max]。"

func parseCodexMessagePreset(text string) (clean string, preset codexMessagePreset, explicit bool, err error) {
	clean = strings.TrimSpace(text)
	if clean == "" {
		return "", preset, false, errors.New("消息正文不能为空")
	}

	if !strings.HasPrefix(clean, "[") {
		return clean, preset, false, nil
	}
	end := strings.Index(clean, "]")
	if end <= 1 {
		return clean, preset, false, nil
	}
	key, effort, hasEffort := strings.Cut(clean[1:end], ":")
	for _, candidate := range []codexMessagePreset{
		{Key: "luna", Model: "gpt-6-luna", ReasoningEffort: "high"},
		{Key: "terra", Model: "gpt-5.6-terra", ReasoningEffort: "high"},
		{Key: "sol", Model: "gpt-6-sol", ReasoningEffort: "high"},
		{Key: "astra", Model: "gpt-6-astra", ReasoningEffort: "high"},
	} {
		if !strings.EqualFold(key, candidate.Key) {
			continue
		}
		preset = candidate
		explicit = true
		clean = strings.TrimSpace(clean[end+1:])
		if hasEffort {
			effort = normalizeModelReasoningEffort(effort)
			if !validCodexMessagePresetReasoningEffort(effort) {
				return clean, preset, true, fmt.Errorf("不支持的推理强度 %s", effort)
			}
			preset.ReasoningEffort = effort
		}
		if clean == "" {
			err = errors.New("消息正文不能为空")
		}
		return
	}

	return clean, preset, false, nil
}

func validCodexMessagePresetReasoningEffort(effort string) bool {
	switch effort {
	case "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}

func (s *Service) codexMessagePresetProfileMode(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) (dynamic, fixed bool) {
	if s.promptConfigBackend(inst, surface) != agentproto.BackendCodex {
		return false, false
	}
	profile, ok := s.surfaceCodexProfileSummary(surface)
	if !ok {
		return false, false
	}
	if _, fixed := fixedCodexAPIProfileModel(profile); fixed {
		return false, true
	}
	switch profile.Kind {
	case state.CodexProfileKindNative, state.CodexProfileKindOAuth:
		return true, false
	case state.CodexProfileKindAPI:
		group, known := s.codexProfileModelGroup(profile)
		return isDynamicCodexProfileModel(profile.BaseURL, profile.Model) && known && group == "gpt", false
	default:
		return false, false
	}
}

func codexMessagePresetOverride(surface *state.SurfaceConsoleRecord, preset codexMessagePreset) state.ModelConfigRecord {
	override := state.ModelConfigRecord{}
	if surface != nil {
		override = surface.PromptOverride
	}
	override.Model = preset.Model
	override.ReasoningEffort = preset.ReasoningEffort
	return override
}

func codexMessagePresetCatalogProblem(inst *state.InstanceRecord, preset codexMessagePreset) string {
	if inst == nil || inst.ModelCatalog == nil || inst.ModelCatalog.Unsupported ||
		strings.TrimSpace(inst.ModelCatalog.ErrorMessage) != "" || strings.TrimSpace(inst.ModelCatalog.NextCursor) != "" {
		return ""
	}
	entry, ok := modelCatalogEntryForModel(inst, preset.Model)
	if !ok {
		return fmt.Sprintf("当前完整模型列表中没有 %s，未发送这条消息。", preset.Model)
	}
	if len(entry.SupportedReasoningEfforts) == 0 {
		return ""
	}
	for _, option := range entry.SupportedReasoningEfforts {
		if normalizeModelReasoningEffort(option.ReasoningEffort) == preset.ReasoningEffort {
			return ""
		}
	}
	return fmt.Sprintf("当前模型 %s 明确不支持推理强度 %s，未发送这条消息。", preset.Model, preset.ReasoningEffort)
}

func stripCodexMessagePresetFromCurrentInputs(inputs, currentInputs []agentproto.Input, preset codexMessagePreset) []agentproto.Input {
	out := append([]agentproto.Input(nil), inputs...)
	start := 0
	if len(currentInputs) != 0 {
		if len(currentInputs) > len(out) {
			return out
		}
		start = len(out) - len(currentInputs)
		for i := range currentInputs {
			if out[start+i] != currentInputs[i] {
				return out
			}
		}
	}
	for i := start; i < len(out); i++ {
		if out[i].Type != agentproto.InputText {
			continue
		}
		clean, candidate, explicit, _ := parseCodexMessagePreset(out[i].Text)
		if explicit && candidate.Key == preset.Key {
			out[i].Text = clean
			return out
		}
	}
	return out
}
