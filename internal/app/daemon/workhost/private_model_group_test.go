package workhost

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateHistoryCodexModelGroupUsesEffectiveConfiguredSource(t *testing.T) {
	policy := &agentproto.CodexResumePolicy{ConnectionContractID: "account", ModelProviderID: "provider", Mode: agentproto.CodexResumeApplyTargetProfile, ModelMode: agentproto.CodexThreadValueExplicit, Model: "gpt-5.5"}
	old := LaunchRecord{Backend: work.BackendCodex, ProfileID: "p", CodexResume: policy, Snapshot: agentproto.RuntimeLaunchSnapshot{HostID: "host", CWD: "/repo", NativeMemoryPolicyVersion: agentproto.NativeMemoryPolicyVersion, Owner: agentproto.ExecutionBinding{BotGeneration: 1, ControllerGeneration: 1}}}
	next := LaunchSettings{Backend: work.BackendCodex, ProfileID: "p", Location: work.ExecutionLocation{ID: "loc", Cwd: "/repo", Generation: 1}, CodexResume: agentproto.CloneCodexResumePolicy(policy)}
	worker := work.WorkerSession{LocationID: "loc", LocationGeneration: 1, ControllerGeneration: 1}
	bot := work.RoleBotBinding{IdentityGeneration: 1}
	for _, tc := range []struct {
		name, oldOverride, nextOverride, nextPolicy string
		want                                        bool
	}{
		{"profile cross group", "", "", "deepseek-v4", false},
		{"override cross group", "gpt-5.5", "deepseek-v4", "gpt-5.5", false},
		{"overrides take precedence", "deepseek-v3", "deepseek-v4", "gpt-5.5", true},
		{"same group", "", "", "o3", true},
		{"unknown target", "", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := old, next
			a.Overrides.Model = tc.oldOverride
			b.Overrides.Model = tc.nextOverride
			b.CodexResume = agentproto.CloneCodexResumePolicy(policy)
			b.CodexResume.Model = tc.nextPolicy
			if got := privateHistoryCompatible(a, b, worker, work.Role{}, bot, "host", nil); got != tc.want {
				t.Fatalf("compatible=%v, want %v", got, tc.want)
			}
		})
	}
	observed := &agentproto.CodexEffectiveThreadContract{ConnectionContractID: "account", ModelProviderID: "provider", Model: "deepseek-v4"}
	if !privateHistoryCompatible(old, next, worker, work.Role{}, bot, "host", observed) {
		t.Fatal("automatic reroute must not trigger a rebuild with unchanged selection")
	}
	next.CodexResume = agentproto.CloneCodexResumePolicy(next.CodexResume)
	next.CodexResume.Model = "gpt-5.6"
	if privateHistoryCompatible(old, next, worker, work.Role{}, bot, "host", observed) {
		t.Fatal("observed non-GPT model must override stale GPT launch settings on explicit switch")
	}
	observed.ConnectionContractID = "other-account"
	if !privateHistoryCompatible(old, next, worker, work.Role{}, bot, "host", observed) {
		t.Fatal("unrelated observation changed compatibility")
	}

}
