package orchestrator

import (
	"reflect"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func privateUpgradeResumeFixture(t *testing.T, newThread bool) (*Service, PrivateUpgradeResume) {
	t.Helper()
	now := time.Now()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.MaterializeSurface("s", "g", "chat", "user")
	root := t.TempDir()
	inst := &state.InstanceRecord{InstanceID: "old", Source: "headless", Managed: true, Online: true, Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID, WorkspaceRoot: root, WorkspaceKey: root, Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: root, WorkspaceKey: root, Loaded: true}}}
	s.UpsertInstance(inst)
	v := s.Surface("s")
	v.ProductMode, v.Backend, v.CodexProfileID = "normal", agentproto.BackendCodex, state.OAuthCodexProfileID
	next := surfaceRouteCoreState{AttachedInstanceID: "old", WorkspaceKey: root, RouteMode: state.RouteModePinned, SelectedThreadID: "thread", ThreadClaimPolicy: surfaceRouteThreadClaimKnown}
	if newThread {
		next.RouteMode, next.SelectedThreadID, next.PreparedThreadCWD = state.RouteModeNewThreadReady, "", root
	}
	if !s.transitionSurfaceRouteCore(v, inst, next) {
		t.Fatal("fixture route")
	}
	s.SetPrivateThreadPolicy("s", PrivateThreadPolicy{InstanceNamespaces: map[string]string{"old": "history"}})
	v.PromptOverride = state.ModelConfigRecord{Model: "chosen-model", AccessMode: "full-access"}
	v.PlanMode, v.PlanModeOverrideSet = state.PlanModeSettingOn, true
	v.DispatchMode = state.DispatchModePausedForLocal
	s.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user", MessageID: "queued", Text: "continue after upgrade"})
	v.DispatchMode = state.DispatchModeNormal
	if len(v.QueuedQueueItemIDs) != 1 {
		t.Fatalf("input not queued: %+v", v.QueuedQueueItemIDs)
	}
	v.StagedImages = map[string]*state.StagedImageRecord{"img-1": {ImageID: "img-1", SurfaceSessionID: "s", LocalPath: "image.png", State: state.ImageStaged}}
	s.nextImageID = 1
	return s, PrivateUpgradeResume{SurfaceID: "s", OldInstanceID: "old", ThreadID: next.SelectedThreadID, CWD: root, ProfileID: state.OAuthCodexProfileID, HistoryNamespace: "history", Backend: agentproto.BackendCodex}
}

func TestPrivateUpgradeCarrierReplacementPreservesConversationAcrossRestart(t *testing.T) {
	for _, newThread := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected-thread", true: "new-thread-ready"}[newThread], func(t *testing.T) {
			s, req := privateUpgradeResumeFixture(t, newThread)
			before, err := s.ExportPrivateCheckpoint("s")
			if err != nil {
				t.Fatal(err)
			}
			previous, err := s.SuspendPrivateUpgradeDispatch("s", "old")
			if err != nil {
				t.Fatal(err)
			}
			events, err := s.PreparePrivateUpgradeResume(req)
			if err != nil || len(events) != 1 || events[0].DaemonCommand == nil {
				t.Fatalf("prepare: %+v %v", events, err)
			}
			command := events[0].DaemonCommand
			if command.Kind != control.DaemonCommandStartHeadless || command.ThreadID != req.ThreadID || command.ThreadCWD != req.CWD || command.CodexProfileID != req.ProfileID {
				t.Fatalf("replacement lost target: %+v", command)
			}
			if _, err = s.PreparePrivateUpgradeResume(req); err == nil {
				t.Fatal("second launch intent manufactured")
			}
			checkpoint, err := s.ExportPrivateCheckpoint("s")
			if err != nil {
				t.Fatal(err)
			}
			restarted := NewService(nil, Config{PrivateApplication: true}, nil)
			if err = restarted.RestorePrivateCheckpoint(checkpoint); err != nil {
				t.Fatal(err)
			}
			wrong := req
			wrong.ThreadID = "foreign"
			if err = restarted.RestorePrivateUpgradeHeadlessProjection(wrong, command.InstanceID); err == nil {
				t.Fatal("wrong pending restored")
			}
			if err = restarted.RestorePrivateUpgradeHeadlessProjection(req, command.InstanceID); err != nil {
				t.Fatal(err)
			}
			if err = restarted.RestorePrivateUpgradeHeadlessProjection(req, command.InstanceID); err != nil {
				t.Fatal(err)
			}
			restarted.SetPrivateThreadPolicy("s", PrivateThreadPolicy{InstanceNamespaces: map[string]string{command.InstanceID: "history"}})
			restarted.UpsertInstance(&state.InstanceRecord{InstanceID: command.InstanceID, Source: "headless", Managed: true, Backend: req.Backend, CodexProfileID: req.ProfileID, WorkspaceRoot: req.CWD, WorkspaceKey: req.CWD, Threads: map[string]*state.ThreadRecord{}})
			for _, event := range restarted.ApplyInstanceConnected(command.InstanceID) {
				if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
					t.Fatal("hello dispatched while maintenance held")
				}
			}
			after := restarted.Surface("s")
			if after.AttachedInstanceID != command.InstanceID || after.SelectedThreadID != before.SelectedThreadID || after.PendingHeadless != nil || after.RouteMode != before.RouteMode {
				t.Fatalf("route changed: %+v", after)
			}
			if !reflect.DeepEqual(after.PromptOverride, before.PromptOverride) || !reflect.DeepEqual(after.QueuedQueueItemIDs, before.QueuedQueueItemIDs) || !reflect.DeepEqual(after.StagedImages, before.StagedImages) || after.PlanMode != before.PlanMode {
				t.Fatal("upgrade changed settings or discarded queue/media")
			}
			if _, err = restarted.ResumePrivateUpgradeDispatch("s", "foreign", previous); err == nil {
				t.Fatal("foreign replacement released")
			}
			if _, err = restarted.ResumePrivateUpgradeDispatch("s", command.InstanceID, state.DispatchModePausedForLocal); err != nil {
				t.Fatal(err)
			}
			if after.DispatchMode != state.DispatchModePausedForLocal || len(after.QueuedQueueItemIDs) != 1 {
				t.Fatal("prior pause not retained")
			}
			continued, err := restarted.ResumePrivateUpgradeDispatch("s", command.InstanceID, state.DispatchModeNormal)
			if err != nil {
				t.Fatal(err)
			}
			var prompts []agentproto.Command
			for _, event := range continued {
				if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
					prompts = append(prompts, *event.Command)
				}
			}
			if len(prompts) != 1 || prompts[0].Target.ThreadID != req.ThreadID || len(prompts[0].Prompt.Inputs) != 1 || prompts[0].Prompt.Inputs[0].Text != "continue after upgrade" {
				t.Fatalf("release did not continue exact queued work: prompts=%+v first=%+v", prompts, continued[0].Command)
			}
		})
	}
}

func TestPrivateUpgradeRejectsChangedIdentityAndUnresolvedExecution(t *testing.T) {
	for _, kind := range []string{"namespace", "profile", "cwd", "thread", "carrier", "active-turn", "pending-input"} {
		t.Run(kind, func(t *testing.T) {
			s, req := privateUpgradeResumeFixture(t, false)
			switch kind {
			case "namespace":
				req.HistoryNamespace = "other"
			case "profile":
				req.ProfileID = "other"
			case "cwd":
				req.CWD = t.TempDir()
			case "thread":
				req.ThreadID = "other"
			case "carrier":
				req.OldInstanceID = "other"
			case "active-turn":
				s.Instance("old").ActiveTurnID = "running"
			case "pending-input":
				s.Surface("s").ActiveQueueItemID = "queue-1"
				s.Surface("s").QueuedQueueItemIDs = nil
			}
			if kind == "active-turn" || kind == "pending-input" {
				if _, err := s.SuspendPrivateUpgradeDispatch("s", "old"); err != nil {
					t.Fatal(err)
				}
				if kind == "active-turn" && s.Instance("old").ActiveTurnID != "running" {
					t.Fatal("pause forged turn completion")
				}
				if kind == "pending-input" && s.Surface("s").ActiveQueueItemID != "queue-1" {
					t.Fatal("pause discarded active input")
				}
			}
			if _, err := s.PreparePrivateUpgradeResume(req); err == nil {
				t.Fatal("unsafe replacement admitted")
			}
			if s.Surface("s").PendingHeadless != nil || !s.Instance("old").Online {
				t.Fatal("failed validation mutated carrier")
			}
		})
	}
}
