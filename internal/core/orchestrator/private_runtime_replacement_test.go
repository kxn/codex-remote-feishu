package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRetiredPrivateRuntimeReplacementKeepsTargetAndContract(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude, agentproto.BackendOpenCode} {
		for _, fresh := range []bool{false, true} {
			t.Run(string(backend)+map[bool]string{false: "/thread", true: "/new"}[fresh], func(t *testing.T) {
				s, req := privateUpgradeResumeFixture(t, fresh)
				v := s.Surface("s")
				inst := s.Instance("old")
				v.Backend = backend
				inst.Backend = backend
				actual := PrivateNativeProfiles{}
				switch backend {
				case agentproto.BackendCodex:
					actual.Codex = state.OAuthCodexProfileID
				case agentproto.BackendClaude:
					actual.Claude = "claude-profile"
					v.ClaudeProfileID = actual.Claude
					inst.ClaudeProfileID = actual.Claude
				case agentproto.BackendOpenCode:
					actual.OpenCode = "opencode-profile"
					v.OpenCodeProfileID = actual.OpenCode
					inst.OpenCodeProfileID = actual.OpenCode
				}
				owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "old", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: backend, HistoryNamespace: "history"}
				s.SetPrivateNativeProfiles("s", actual)
				s.AuthenticatePrivateNavigation("s", owner)
				cp, err := s.ExportPrivateCheckpoint("s")
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now()
				r := newServiceForTest(&now)
				r.config.PrivateApplication = true
				if err = r.RestorePrivateCheckpoint(cp); err != nil {
					t.Fatal(err)
				}
				r.SetPrivateThreadPolicy("s", PrivateThreadPolicy{InstanceNamespaces: map[string]string{"old": "history"}})
				contract := PrivateRetiredContract{Profiles: actual, ClaudeReasoningEffort: "high", OpenCodeRuntimeAccessMode: "full-access"}
				events, err := r.MaterializeRetiredPrivateReplacement("s", owner, contract, req.CWD)
				if err != nil {
					t.Fatal(err)
				}
				if len(events) != 1 || events[0].DaemonCommand == nil {
					t.Fatal("missing durable launch")
				}
				cmd := events[0].DaemonCommand
				if cmd.Backend != backend || cmd.ThreadID != req.ThreadID || cmd.ThreadCWD != req.CWD || !cmd.AutoRestore {
					t.Fatalf("wrong target: %+v", cmd)
				}
				if backend == agentproto.BackendClaude && cmd.ClaudeReasoningEffort != "high" {
					t.Fatal("reasoning lost")
				}
				if backend == agentproto.BackendOpenCode && cmd.OpenCodeRuntimeAccessMode != "full-access" {
					t.Fatal("access lost")
				}
				if len(r.Surface("s").QueuedQueueItemIDs) != 1 {
					t.Fatal("queue lost")
				}
			})
		}
	}
}

func TestRetiredPrivateReplacementAllowsUserDetach(t *testing.T) {
	s, req := privateUpgradeResumeFixture(t, false)
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "old", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	actual := PrivateNativeProfiles{Codex: req.ProfileID}
	s.SetPrivateNativeProfiles("s", actual)
	s.AuthenticatePrivateNavigation("s", owner)
	cp, err := s.ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r := newServiceForTest(&now)
	r.config.PrivateApplication = true
	if err = r.RestorePrivateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	r.SetPrivateThreadPolicy("s", PrivateThreadPolicy{InstanceNamespaces: map[string]string{"old": "history"}})
	if _, err = r.MaterializeRetiredPrivateReplacement("s", owner, PrivateRetiredContract{Profiles: actual}, req.CWD); err != nil {
		t.Fatal(err)
	}
	action := control.Action{Kind: control.ActionDetach, SurfaceSessionID: "s", GatewayID: "g", ChatID: "chat", ActorUserID: "user"}
	events := r.ApplySurfaceAction(action)
	if r.Surface("s").AttachedInstanceID != "" {
		for _, e := range events {
			if e.Notice != nil {
				t.Logf("notice=%s", e.Notice.Code)
			}
		}
		t.Fatal("detach did not clear old navigation")
	}
}

func TestRetiredReplacementDetachIsolatesPendingCarrierWithoutQueuedHold(t *testing.T) {
	s, req := privateUpgradeResumeFixture(t, false)
	v := s.Surface("s")
	v.QueuedQueueItemIDs = nil
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "old", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	actual := PrivateNativeProfiles{Codex: req.ProfileID}
	s.SetPrivateNativeProfiles("s", actual)
	s.AuthenticatePrivateNavigation("s", owner)
	cp, err := s.ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r := newServiceForTest(&now)
	r.config.PrivateApplication = true
	if err = r.RestorePrivateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	r.SetPrivateThreadPolicy("s", PrivateThreadPolicy{InstanceNamespaces: map[string]string{"old": "history"}})
	events, err := r.MaterializeRetiredPrivateReplacement("s", owner, PrivateRetiredContract{Profiles: actual}, req.CWD)
	if err != nil {
		t.Fatal(err)
	}
	newID := events[0].DaemonCommand.InstanceID
	events = r.ApplySurfaceAction(control.Action{Kind: control.ActionDetach, SurfaceSessionID: "s", GatewayID: "g", ChatID: "chat", ActorUserID: "user"})
	if r.Surface("s").AttachedInstanceID != "" || !r.PrivateDetachedInstance("s", newID) {
		t.Fatal("pending replacement not isolated")
	}
	kill := false
	for _, e := range events {
		if e.DaemonCommand != nil && e.DaemonCommand.Kind == control.DaemonCommandKillHeadless && e.DaemonCommand.InstanceID == newID {
			kill = true
		}
	}
	if !kill {
		t.Fatal("wrong cancellation target")
	}
	saved, err := r.ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	restored := newServiceForTest(&now)
	restored.config.PrivateApplication = true
	if err = restored.RestorePrivateCheckpoint(saved); err != nil {
		t.Fatalf("detached checkpoint invalid: %v", err)
	}
}

func TestPrivateStopProjectionPreservesUserDispatchPreference(t *testing.T) {
	for _, mode := range []state.DispatchMode{state.DispatchModeNormal, state.DispatchModePausedForLocal} {
		s, _ := privateUpgradeResumeFixture(t, false)
		s.config.PrivateApplication = true
		v := s.Surface("s")
		v.DispatchMode = mode
		inst := s.Instance(v.AttachedInstanceID)
		inst.Online = true
		if err := s.MarkPrivateStopProjectionOffline("s", inst.InstanceID); err != nil {
			t.Fatal(err)
		}
		if inst.Online || v.DispatchMode != mode {
			t.Fatalf("online=%v mode=%v", inst.Online, v.DispatchMode)
		}
	}
}
