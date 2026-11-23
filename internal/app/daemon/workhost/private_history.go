package workhost

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A carrier's mutable execution policy can change without discarding private
// native history. Physical ownership and the provider connection remain exact.
func privateHistoryCompatible(old LaunchRecord, next LaunchSettings, worker work.WorkerSession, role work.Role, bot work.RoleBotBinding, host string, observed *agentproto.CodexEffectiveThreadContract) bool {
	s := old.Snapshot
	if old.Backend != next.Backend || old.ProfileID != next.ProfileID ||
		s.HostID != host || s.CWD != next.Location.Cwd ||
		worker.LocationID != next.Location.ID || worker.LocationGeneration != next.Location.Generation ||
		s.NativeBinaryPath != next.NativeBinaryPath || s.NativeBinaryVersion != next.NativeBinaryVersion ||
		s.NativeMemoryPolicyVersion != agentproto.NativeMemoryPolicyVersion || s.ReadOnly != next.ReadOnly ||
		s.Owner.BotGeneration != bot.IdentityGeneration || s.Owner.ControllerGeneration != worker.ControllerGeneration {
		return false
	}
	if old.Backend == work.BackendCodex {
		oldModel := privateCodexSelectedModel(old.Overrides, old.CodexResume)
		nextModel := privateCodexSelectedModel(next.Overrides, next.CodexResume)
		oldGroup := agentproto.CodexModelGroup(oldModel)
		if observed != nil && old.CodexResume != nil && observed.ConnectionContractID == old.CodexResume.ConnectionContractID && (old.CodexResume.InheritNativeProvider || observed.ModelProviderID == old.CodexResume.ModelProviderID) {
			if group := agentproto.CodexModelGroup(observed.Model); group != "" {
				oldGroup = group
			}
		}
		nextGroup := agentproto.CodexModelGroup(nextModel)
		if oldModel != nextModel && oldGroup != "" && nextGroup != "" && oldGroup != nextGroup {
			return false
		}
		a, b := old.CodexResume, next.CodexResume
		if a != nil || b != nil {
			return a != nil && b != nil && a.ConnectionContractID != "" && a.ConnectionContractID == b.ConnectionContractID && a.ModelProviderID == b.ModelProviderID && a.InheritNativeProvider == b.InheritNativeProvider && a.Mode == b.Mode
		}
	}
	if old.ConnectionIdentity != "" {
		return old.ConnectionIdentity == next.ConnectionIdentity
	}
	// Legacy records contain no independent connection evidence. Unchanged role
	// settings still permit context-local permissions and memory to evolve.
	return old.SettingsRevision == role.SettingsRevision
}

// The translator applies prompt overrides after the explicit profile policy.
// An unspecified native/default model is unknown, not an inferred GPT model.
func privateCodexSelectedModel(overrides agentproto.PromptOverrides, policy *agentproto.CodexResumePolicy) string {
	model := strings.TrimSpace(overrides.Model)
	if model == "" && policy != nil && policy.ModelMode == agentproto.CodexThreadValueExplicit {
		model = strings.TrimSpace(policy.Model)
	}
	return strings.ToLower(model)
}
