package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func CodexResumePolicyFromContract(connection *state.CodexConnectionContract, threadPolicy *state.CodexThreadPolicy) *agentproto.CodexResumePolicy {
	connection = state.CloneCodexConnectionContract(connection)
	threadPolicy = state.CloneCodexThreadPolicy(threadPolicy)
	if connection == nil || strings.TrimSpace(connection.ModelProviderID) == "" {
		return nil
	}
	policy := &agentproto.CodexResumePolicy{
		Mode:                 agentproto.CodexResumeApplyTargetProfile,
		ConnectionContractID: connection.ConnectionContractID,
		ModelProviderID:      connection.ModelProviderID,
	}
	if threadPolicy != nil {
		policy.ThreadPolicyID = threadPolicy.ThreadPolicyID
		policy.ModelMode = threadPolicy.ModelMode
		policy.Model = threadPolicy.Model
		policy.ReviewModelMode = threadPolicy.ReviewModelMode
		policy.ReviewModel = threadPolicy.ReviewModel
		policy.ReasoningMode = threadPolicy.ReasoningMode
		policy.ReasoningEffort = threadPolicy.ReasoningEffort
		policy.DeveloperInstruction = threadPolicy.DeveloperInstruction
		policy.ContextMode = threadPolicy.ContextMode
		policy.ContextWindow = threadPolicy.ContextWindow
		policy.AutoCompactLimit = threadPolicy.AutoCompactLimit
	}
	return agentproto.NormalizeCodexResumePolicy(policy)
}

func CodexResumePolicyForThread(connection *state.CodexConnectionContract, threadPolicy *state.CodexThreadPolicy, thread *state.ThreadRecord) *agentproto.CodexResumePolicy {
	connection = state.CloneCodexConnectionContract(connection)
	threadPolicy = state.CloneCodexThreadPolicy(threadPolicy)
	if connection == nil || strings.TrimSpace(connection.ModelProviderID) == "" {
		return nil
	}
	if preserved := codexPreservePolicyFromObserved(connection, threadPolicy, thread); preserved != nil {
		return preserved
	}
	return CodexResumePolicyFromContract(connection, threadPolicy)
}

func codexPreservePolicyFromObserved(connection *state.CodexConnectionContract, threadPolicy *state.CodexThreadPolicy, thread *state.ThreadRecord) *agentproto.CodexResumePolicy {
	if connection == nil || thread == nil || thread.CodexEffectiveThread == nil {
		return nil
	}
	observed := agentproto.CloneCodexEffectiveThreadContract(thread.CodexEffectiveThread)
	if strings.TrimSpace(observed.ModelProviderID) == "" || strings.TrimSpace(observed.ModelProviderID) != strings.TrimSpace(connection.ModelProviderID) {
		return nil
	}
	if strings.TrimSpace(observed.ConnectionContractID) != "" && strings.TrimSpace(connection.ConnectionContractID) != "" &&
		strings.TrimSpace(observed.ConnectionContractID) != strings.TrimSpace(connection.ConnectionContractID) {
		return nil
	}
	policy := &agentproto.CodexResumePolicy{
		Mode:                 agentproto.CodexResumePreserveThreadSettings,
		ConnectionContractID: connection.ConnectionContractID,
		ModelProviderID:      connection.ModelProviderID,
		ModelMode:            agentproto.CodexThreadValueDefault,
		ReviewModelMode:      agentproto.CodexReviewModelConfig,
		ReasoningMode:        agentproto.CodexThreadValueDefault,
	}
	if threadPolicy != nil {
		policy.ThreadPolicyID = threadPolicy.ThreadPolicyID
		policy.ReviewModelMode = threadPolicy.ReviewModelMode
		policy.ReviewModel = threadPolicy.ReviewModel
		policy.DeveloperInstruction = threadPolicy.DeveloperInstruction
		policy.ContextMode = threadPolicy.ContextMode
		policy.ContextWindow = threadPolicy.ContextWindow
		policy.AutoCompactLimit = threadPolicy.AutoCompactLimit
	}
	if strings.TrimSpace(observed.Model) != "" {
		policy.ModelMode = agentproto.CodexThreadValuePreservedObserved
		policy.Model = strings.TrimSpace(observed.Model)
	}
	if strings.TrimSpace(observed.ReasoningEffort) != "" {
		policy.ReasoningMode = agentproto.CodexThreadValuePreservedObserved
		policy.ReasoningEffort = strings.TrimSpace(observed.ReasoningEffort)
	}
	return agentproto.NormalizeCodexResumePolicy(policy)
}

func codexResumeTargetThread(inst *state.InstanceRecord, dispatchPlan agentproto.PromptDispatchPlan) *state.ThreadRecord {
	if inst == nil {
		return nil
	}
	return codexResumeThreadByID(inst, dispatchPlan.EffectiveExecutionThreadID())
}

func codexResumeThreadByID(inst *state.InstanceRecord, threadID string) *state.ThreadRecord {
	if inst == nil || inst.Threads == nil {
		return nil
	}
	return inst.Threads[strings.TrimSpace(threadID)]
}
