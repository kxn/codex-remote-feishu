package workhost

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type mutablePrivateSettings struct{ settings LaunchSettings }

func (s *mutablePrivateSettings) ResolveLaunch(context.Context, work.WorkScope, work.Role, work.WorkContext) (LaunchSettings, error) {
	return s.settings, nil
}

func TestPrivatePolicyRebuildPreservesNativeSessionButExplicitNewDoesNot(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "settings", true: "new"}[fresh], func(t *testing.T) {
			calls := 0
			h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { calls++; return 1234, nil })
			initial, _ := h.config.Settings.ResolveLaunch(context.Background(), work.WorkScope{}, work.Role{}, work.WorkContext{})
			initial.ConnectionIdentity = "same-auth-and-provider"
			settings := &mutablePrivateSettings{settings: initial}
			h.config.Settings = settings
			need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
			if err := h.Prepare(context.Background(), need); err != nil {
				t.Fatal(err)
			}
			h.config.Runtime = &rebuildDriver{stopped: true}
			before := seedIdleSession(t, h, store)
			readyAndChangeRole(t, store)

			settings.settings.Overrides = agentproto.PromptOverrides{AccessMode: agentproto.AccessModeFullAccess, Model: "new-model", ReasoningEffort: "high"}
			h.config.Runtime = &rebuildDriver{stopped: true}
			h.config.StopCarrier = func(context.Context, work.WorkerSession) error { return nil }
			ctx := context.Background()
			if fresh {
				ctx = context.WithValue(ctx, freshHistoryKey{}, true)
			}
			if err := h.Prepare(ctx, need); err != nil {
				t.Fatal(err)
			}
			after := hostWorker(t, store)
			want := before.NativeSessionID
			if fresh {
				want = ""
			}
			if calls != 2 || after.NativeSessionID != want {
				t.Fatalf("launches=%d session=%q want=%q", calls, after.NativeSessionID, want)
			}
		})
	}
}

func TestPrivateHistoryRequiresExactConnectionAndOwnership(t *testing.T) {
	old := LaunchRecord{Backend: work.BackendCodex, ProfileID: "p", SettingsRevision: 1, CodexResume: &agentproto.CodexResumePolicy{ConnectionContractID: "account-a", ModelProviderID: "openai", Mode: agentproto.CodexResumeApplyTargetProfile}, Snapshot: agentproto.RuntimeLaunchSnapshot{HostID: "host", CWD: "/repo", NativeMemoryPolicyVersion: agentproto.NativeMemoryPolicyVersion, Owner: agentproto.ExecutionBinding{BotGeneration: 1, ControllerGeneration: 1}}}
	next := LaunchSettings{Backend: work.BackendCodex, ProfileID: "p", Location: work.ExecutionLocation{ID: "loc", Cwd: "/repo", Generation: 1}, CodexResume: agentproto.CloneCodexResumePolicy(old.CodexResume)}
	worker := work.WorkerSession{LocationID: "loc", LocationGeneration: 1, ControllerGeneration: 1}
	role := work.Role{SettingsRevision: 2}
	bot := work.RoleBotBinding{IdentityGeneration: 1}
	next.CodexResume.Model = "new-model"
	next.CodexResume.ThreadPolicyID = "new-thread-policy"
	if !privateHistoryCompatible(old, next, worker, role, bot, "host") {
		t.Fatal("mutable model lost history")
	}
	for _, change := range []string{"account", "provider", "profile", "cwd", "bot", "host", "policy"} {
		t.Run(change, func(t *testing.T) {
			n := next
			n.CodexResume = agentproto.CloneCodexResumePolicy(next.CodexResume)
			b := bot
			host := "host"
			switch change {
			case "account":
				n.CodexResume.ConnectionContractID = "other"
			case "provider":
				n.CodexResume.ModelProviderID = "other"
			case "profile":
				n.ProfileID = "other"
			case "cwd":
				n.Location.Cwd = "/other"
			case "bot":
				b.IdentityGeneration++
			case "host":
				host = "other"
			case "policy":
				n.ReadOnly = true
			}
			if privateHistoryCompatible(old, n, worker, role, b, host) {
				t.Fatal("identity change reused history")
			}
		})
	}
}
