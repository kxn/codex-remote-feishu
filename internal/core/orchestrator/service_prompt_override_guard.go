package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type promptOverrideGuardResult struct {
	DroppedModel     bool
	DroppedReasoning bool
	Model            string
	FixedModel       string
	ReasoningEffort  string
	SupportedEfforts []string
}

func (s *Service) sanitizePromptOverridesForDispatch(surface *state.SurfaceConsoleRecord, dispatchPlan agentproto.PromptDispatchPlan, override state.ModelConfigRecord) (state.ModelConfigRecord, promptOverrideGuardResult) {
	override = compactPromptOverride(override)
	if surface == nil {
		return override, promptOverrideGuardResult{}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	backend := s.promptConfigBackend(inst, surface)
	if !state.BackendAcceptsFeishuPromptOverrides(backend) {
		if agentproto.NormalizeBackend(backend) == agentproto.BackendOpenCode {
			normalized := state.NormalizePromptOverrideForBackend(backend, override)
			normalized.AccessMode = ""
			override = compactPromptOverride(normalized)
			if strings.TrimSpace(override.ReasoningEffort) == "" {
				return override, promptOverrideGuardResult{}
			}
			validationModel := override.Model
			if strings.TrimSpace(validationModel) == "" {
				validationModel = s.threadObservedModel(inst, agentproto.NormalizePromptDispatchPlan(dispatchPlan).EffectiveExecutionThreadID())
			}
			validation := s.checkModelReasoningSupport(inst, validationModel, override.ReasoningEffort)
			if validation.Support != modelReasoningSupported {
				guard := promptOverrideGuardResult{
					DroppedReasoning: true,
					Model:            strings.TrimSpace(validationModel),
					ReasoningEffort:  strings.TrimSpace(override.ReasoningEffort),
					SupportedEfforts: append([]string(nil), validation.SupportedEfforts...),
				}
				override.ReasoningEffort = ""
				return compactPromptOverride(override), guard
			}
			return compactPromptOverride(override), promptOverrideGuardResult{}
		}
		return state.ModelConfigRecord{}, promptOverrideGuardResult{}
	}
	if agentproto.NormalizeBackend(backend) == agentproto.BackendClaude {
		return override, promptOverrideGuardResult{}
	}
	if profile, ok := s.surfaceCodexProfileSummary(surface); ok {
		if fixedModel, fixed := fixedCodexAPIProfileModel(profile); fixed {
			overrideModel := strings.TrimSpace(override.Model)
			overrideEffort := strings.TrimSpace(override.ReasoningEffort)
			override.Model = ""
			override.ReasoningEffort = ""
			if overrideModel != "" && !strings.EqualFold(overrideModel, fixedModel) {
				return compactPromptOverride(override), promptOverrideGuardResult{
					DroppedModel:    true,
					Model:           overrideModel,
					FixedModel:      fixedModel,
					ReasoningEffort: overrideEffort,
				}
			}
			return compactPromptOverride(override), promptOverrideGuardResult{}
		}
	}
	if strings.TrimSpace(override.ReasoningEffort) == "" {
		return override, promptOverrideGuardResult{}
	}
	validation := s.checkModelReasoningSupport(inst, override.Model, override.ReasoningEffort)
	if validation.Support == modelReasoningUnsupported {
		guard := promptOverrideGuardResult{
			DroppedReasoning: true,
			Model:            strings.TrimSpace(override.Model),
			ReasoningEffort:  strings.TrimSpace(override.ReasoningEffort),
			SupportedEfforts: append([]string(nil), validation.SupportedEfforts...),
		}
		override.ReasoningEffort = ""
		return compactPromptOverride(override), guard
	}
	return compactPromptOverride(override), promptOverrideGuardResult{}
}

func (s *Service) threadObservedModel(inst *state.InstanceRecord, threadID string) string {
	if inst == nil {
		return ""
	}
	thread := inst.Threads[strings.TrimSpace(threadID)]
	if thread == nil {
		return ""
	}
	if strings.TrimSpace(thread.ExplicitModel) != "" {
		return strings.TrimSpace(thread.ExplicitModel)
	}
	if thread.ThreadSettings != nil && strings.TrimSpace(thread.ThreadSettings.Model) != "" {
		return strings.TrimSpace(thread.ThreadSettings.Model)
	}
	return ""
}

func modelReasoningGuardNoticeEvent(surface *state.SurfaceConsoleRecord, guard promptOverrideGuardResult) eventcontract.Event {
	model := strings.TrimSpace(guard.Model)
	effort := strings.TrimSpace(guard.ReasoningEffort)
	text := "当前模型不支持已保存的推理强度覆盖，已改用模型默认思考强度。"
	if model != "" && effort != "" {
		text = "当前模型 " + model + " 不支持已保存的推理强度 " + effort + "，已改用模型默认思考强度。"
	}
	if len(guard.SupportedEfforts) != 0 {
		text += " 当前模型支持：" + strings.Join(guard.SupportedEfforts, "、") + "。"
	}
	dedupKey := "prompt_override_guard:" + model + ":" + effort
	return surfaceEventFromPayload(
		surface,
		eventcontract.NoticePayload{Notice: control.Notice{
			Code:             "prompt_override_reasoning_dropped",
			Text:             text,
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilyPromptOverrideGuard,
			DeliveryDedupKey: dedupKey,
		}},
		eventcontract.EventMeta{},
	)
}

func modelOverrideGuardNoticeEvent(surface *state.SurfaceConsoleRecord, guard promptOverrideGuardResult) eventcontract.Event {
	model := strings.TrimSpace(guard.Model)
	fixedModel := strings.TrimSpace(guard.FixedModel)
	text := "当前 Codex Profile 使用固定模型，已忽略不匹配的模型覆盖。"
	if model != "" && fixedModel != "" {
		text = "当前 Codex Profile 使用固定模型 " + fixedModel + "，已忽略不匹配的模型覆盖 " + model + "。"
	}
	dedupKey := "prompt_override_model:" + model + ":" + fixedModel
	return surfaceEventFromPayload(
		surface,
		eventcontract.NoticePayload{Notice: control.Notice{
			Code:             "prompt_override_model_dropped",
			Text:             text,
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilyPromptOverrideGuard,
			DeliveryDedupKey: dedupKey,
		}},
		eventcontract.EventMeta{},
	)
}
