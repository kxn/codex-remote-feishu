package orchestrator

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexPermissionProfileGrantAndDeny(t *testing.T) {
	profile := map[string]any{"fileSystem": map[string]any{"entries": []any{map[string]any{"access": "write", "path": map[string]any{"type": "path", "path": "/workspace"}}}}, "network": map[string]any{"enabled": true}}
	request := &state.RequestPromptRecord{Backend: agentproto.BackendCodex, Prompt: &agentproto.RequestPrompt{Permissions: &agentproto.PermissionsRequestPrompt{Profile: profile}}}
	for _, tc := range []struct {
		option, scope string
		want          any
	}{{"accept", "turn", profile}, {"acceptForSession", "session", profile}, {"decline", "turn", map[string]any{}}} {
		response, ok, _ := buildPermissionsRequestResponse(request, control.Action{Request: &control.ActionRequestResponse{RequestOptionID: tc.option}})
		if !ok || response["scope"] != tc.scope || !reflect.DeepEqual(response["permissions"], tc.want) {
			t.Fatalf("%s: %#v", tc.option, response)
		}
	}
	profile["network"].(map[string]any)["unrecognized"] = true
	response, ok, _ := buildPermissionsRequestResponse(request, control.Action{Request: &control.ActionRequestResponse{RequestOptionID: "accept"}})
	if ok || response != nil {
		t.Fatal("unknown permission was granted")
	}
	response, ok, _ = buildPermissionsRequestResponse(request, control.Action{Request: &control.ActionRequestResponse{RequestOptionID: "decline"}})
	if !ok || !reflect.DeepEqual(response["permissions"], map[string]any{}) {
		t.Fatal("invalid request could not be safely declined")
	}
}
func TestNonblockingRequestRemainsPendingWithoutBlockingWork(t *testing.T) {
	no := false
	yes := true
	surface := &state.SurfaceConsoleRecord{PendingRequests: map[string]*state.RequestPromptRecord{"nonblocking": {RequestID: "nonblocking", Prompt: &agentproto.RequestPrompt{Type: agentproto.RequestTypeRequestUserInput, IsBlocking: &no}}}, PendingRequestOrder: []string{"nonblocking"}}
	if activePendingRequest(surface) == nil || blockingPendingRequest(surface) != nil {
		t.Fatal("nonblocking request not retained independently")
	}
	surface.PendingRequests["blocking"] = &state.RequestPromptRecord{RequestID: "blocking", Prompt: &agentproto.RequestPrompt{Type: agentproto.RequestTypeRequestUserInput, IsBlocking: &yes}}
	surface.PendingRequestOrder = append(surface.PendingRequestOrder, "blocking")
	if got := blockingPendingRequest(surface); got == nil || got.RequestID != "blocking" {
		t.Fatal("later blocking request ignored")
	}
}

func TestUserVerificationCannotBeAcceptedWithoutDeviceProof(t *testing.T) {
	prompt := &agentproto.RequestPrompt{Type: agentproto.RequestTypeMCPServerElicitation, MCPElicitation: &agentproto.MCPElicitationPrompt{Mode: "openai/userVerification"}}
	if got := buildMCPElicitationQuestions(prompt, nil); len(got) != 0 {
		t.Fatal("device verification offered a fake input form")
	}
	opts := buildMCPElicitationOptions(prompt, nil, nil)
	if len(opts) != 2 || opts[0].OptionID != "decline" || opts[1].OptionID != "cancel" {
		t.Fatalf("unsupported options: %+v", opts)
	}
	svc, surface, _ := newReviewSessionService(t)
	req := &state.RequestPromptRecord{Prompt: prompt}
	response, ok, _ := svc.buildMCPElicitationResponse(surface, req, control.Action{Request: &control.ActionRequestResponse{RequestOptionID: "accept"}})
	if ok || response != nil {
		t.Fatal("accepted device verification without proof")
	}
	response, ok, _ = svc.buildMCPElicitationResponse(surface, req, control.Action{Request: &control.ActionRequestResponse{RequestOptionID: "decline"}})
	if !ok || response["action"] != "decline" {
		t.Fatal("could not decline unsupported verification")
	}
}

func TestEmptyPermissionProfileKeepsObjectAfterRestart(t *testing.T) {
	original := agentproto.RequestPrompt{Permissions: &agentproto.PermissionsRequestPrompt{ProfilePresent: true, Profile: map[string]any{}}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored agentproto.RequestPrompt
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	req := &state.RequestPromptRecord{Backend: agentproto.BackendCodex, Prompt: &restored}
	response, ok, _ := buildPermissionsRequestResponse(req, control.Action{Request: &control.ActionRequestResponse{RequestOptionID: "decline"}})
	if !ok || !reflect.DeepEqual(response["permissions"], map[string]any{}) {
		t.Fatalf("lost empty object: %#v", response)
	}
}

func TestCodexMissingOrLegacyPermissionsCannotGrantButCanDecline(t *testing.T) {
	for _, prompt := range []*agentproto.RequestPrompt{nil, {Permissions: &agentproto.PermissionsRequestPrompt{Permissions: []map[string]any{{"name": "unverified"}}}}, {Permissions: &agentproto.PermissionsRequestPrompt{ProfilePresent: true, ProfileInvalid: true}}} {
		req := &state.RequestPromptRecord{Backend: agentproto.BackendCodex, Prompt: prompt}
		for _, option := range []string{"accept", "acceptForSession"} {
			result, ok, _ := buildPermissionsRequestResponse(req, control.Action{Request: &control.ActionRequestResponse{RequestOptionID: option}})
			if ok || result != nil {
				t.Fatalf("unverified grant: %#v", result)
			}
		}
		result, ok, _ := buildPermissionsRequestResponse(req, control.Action{Request: &control.ActionRequestResponse{RequestOptionID: "decline"}})
		if !ok || !reflect.DeepEqual(result["permissions"], map[string]any{}) {
			t.Fatalf("unsafe denial: %#v", result)
		}
	}
}

func TestOnlyExplicitNonblockingToolInputSkipsGate(t *testing.T) {
	no := false
	for _, prompt := range []*agentproto.RequestPrompt{nil, {Type: agentproto.RequestTypeRequestUserInput}, {Type: agentproto.RequestTypeApproval, IsBlocking: &no}, {Type: agentproto.RequestTypeMCPServerElicitation, IsBlocking: &no}} {
		surface := &state.SurfaceConsoleRecord{PendingRequests: map[string]*state.RequestPromptRecord{"request": {RequestID: "request", Prompt: prompt}}, PendingRequestOrder: []string{"request"}}
		if blockingPendingRequest(surface) == nil {
			t.Fatalf("request bypassed gate: %#v", prompt)
		}
	}
}

func TestReviewApprovalRequiresExactProvenRoot(t *testing.T) {
	svc, surface, cwd := newReviewSessionService(t)
	svc.config.PrivateApplication = true
	startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)
	event := reviewApprovalRequestEvent("thread-review", "child-turn", "child-request", control.RequestSemanticApprovalCommand)
	event.NativeBinding = &agentproto.NativeTurnBinding{}
	event.RootTurnID = "turn-review-1"
	result := svc.ApplyAgentEvent("inst-1", event)
	requireSilentReviewApprovalCommand(t, result, "child-request")
	other, otherSurface, otherCWD := newReviewSessionService(t)
	other.config.PrivateApplication = true
	startCodexReviewForAutoApprovalTest(t, other, otherSurface, otherCWD)
	event.RootTurnID = "foreign-root"
	event.RequestID = "foreign-request"
	requireReviewRequestPrompt(t, other.ApplyAgentEvent("inst-1", event), "foreign-request")
}

func TestSnapshotDistinguishesNonblockingAndMixedRequests(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	no := false
	surface.PendingRequests = map[string]*state.RequestPromptRecord{"optional": {RequestID: "optional", Prompt: &agentproto.RequestPrompt{Type: agentproto.RequestTypeRequestUserInput, IsBlocking: &no}}}
	surface.PendingRequestOrder = []string{"optional"}
	if got := svc.snapshotGateSummary(surface); got.Kind != "nonblocking_request" {
		t.Fatalf("nonblocking summary: %+v", got)
	}
	surface.PendingRequests["approval"] = &state.RequestPromptRecord{RequestID: "approval", Prompt: &agentproto.RequestPrompt{Type: agentproto.RequestTypeApproval}}
	surface.PendingRequestOrder = append(surface.PendingRequestOrder, "approval")
	if got := svc.snapshotGateSummary(surface); got.Kind != "pending_request" {
		t.Fatalf("mixed summary: %+v", got)
	}
}

func TestBootstrapRequestCommitsOnlyProvenMatchingRunningTurn(t *testing.T) {
	for _, tc := range []struct {
		name         string
		binding      bool
		thread, turn string
		want         bool
	}{{"proven", true, "new-thread", "turn", true}, {"unproven", false, "new-thread", "turn", false}, {"foreign-thread", true, "other-thread", "turn", false}, {"foreign-turn", true, "new-thread", "other-turn", false}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, surface, cwd := newReviewSessionService(t)
			svc.config.PrivateApplication = true
			surface.RouteMode = state.RouteModeNewThreadReady
			surface.SelectedThreadID = ""
			surface.PreparedThreadCWD = cwd
			surface.ActiveQueueItemID = "q"
			surface.QueueItems = map[string]*state.QueueItemRecord{"q": {ID: "q", SurfaceSessionID: surface.SurfaceSessionID, Status: state.QueueItemRunning, RouteModeAtEnqueue: state.RouteModeNewThreadReady}}
			binding := &remoteTurnBinding{InstanceID: "inst-1", SurfaceSessionID: surface.SurfaceSessionID, QueueItemID: "q", BootstrapNewThread: true, ThreadID: "new-thread", TurnID: "turn"}
			svc.bindActiveRemoteTurn("inst-1", binding)
			event := agentproto.Event{Kind: agentproto.EventRequestStarted, ThreadID: tc.thread, TurnID: tc.turn, RequestID: "request", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID}, RequestPrompt: &agentproto.RequestPrompt{Type: agentproto.RequestTypeApproval}, Metadata: map[string]any{"requestType": "approval", "title": "Approve?"}}
			if tc.binding {
				event.NativeBinding = &agentproto.NativeTurnBinding{}
			}
			svc.presentRequestPrompt("inst-1", event)
			if got := surface.RouteMode == state.RouteModePinned && surface.SelectedThreadID == "new-thread"; got != tc.want {
				t.Fatalf("route=%s thread=%s committed=%v inst=%v active=%+v exec=%q surface=%q", surface.RouteMode, surface.SelectedThreadID, binding.ThreadCommitted, svc.root.Instances["inst-1"] != nil, svc.activeRemoteBinding("inst-1", tc.turn), remoteBindingExecutionThreadID(binding), surface.SurfaceSessionID)
			}
			if binding.ThreadCommitted != tc.want {
				t.Fatalf("committed=%v want=%v", binding.ThreadCommitted, tc.want)
			}
		})
	}
}

func TestCodexPermissionProfileRejectsUnknownNullAndUnsafeDepth(t *testing.T) {
	for _, profile := range []map[string]any{{"unknown": nil}, {"fileSystem": map[string]any{"unknown": nil}}, {"network": map[string]any{"unknown": nil}}, {"fileSystem": map[string]any{"globScanMaxDepth": math.Inf(1)}}, {"fileSystem": map[string]any{"globScanMaxDepth": float64(9007199254740992)}}} {
		if validCodexPermissionProfile(profile) {
			t.Fatalf("unsafe profile accepted: %#v", profile)
		}
	}
}

func TestBootstrapCommitKeepsUncommittedOnConflictingClaimAndRetries(t *testing.T) {
	svc, surface, cwd := newReviewSessionService(t)
	svc.config.PrivateApplication = true
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.SelectedThreadID = ""
	surface.PreparedThreadCWD = cwd
	inst := svc.root.Instances["inst-1"]
	svc.MaterializeSurface("foreign", "gateway", "other-chat", "other-user")
	foreign := svc.root.Surfaces["foreign"]
	foreign.AttachedInstanceID = "inst-1"
	foreign.SelectedThreadID = "new-thread"
	svc.bindThreadClaim(foreign, "inst-1", "new-thread")
	item := &state.QueueItemRecord{ID: "q", SurfaceSessionID: surface.SurfaceSessionID, Status: state.QueueItemRunning, RouteModeAtEnqueue: state.RouteModeNewThreadReady}
	binding := &remoteTurnBinding{InstanceID: "inst-1", SurfaceSessionID: surface.SurfaceSessionID, QueueItemID: "q", BootstrapNewThread: true, ThreadID: "new-thread", TurnID: "turn"}
	svc.maybeCommitBootstrapSurfaceBinding(surface, inst, item, binding, "new-thread")
	if binding.ThreadCommitted || surface.RouteMode != state.RouteModeNewThreadReady {
		t.Fatal("failed claim marked committed")
	}
	svc.releaseSurfaceThreadClaim(foreign)
	svc.maybeCommitBootstrapSurfaceBinding(surface, inst, item, binding, "new-thread")
	if !binding.ThreadCommitted || surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "new-thread" {
		t.Fatalf("valid retry failed: %+v", binding)
	}
}

func TestOwnershipLossSettlesExactGoalProbeWithoutReplayingQueue(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	svc.config.PrivateApplication = true
	lease := &GoalInterlockLease{InstanceID: "inst-1", ThreadID: "thread-main", TriggerSurfaceID: surface.SurfaceSessionID, Phase: GoalInterlockQuiescing, ProbeCommandID: "probe", TriggerQueueItemID: "queue-1"}
	svc.setGoalInterlockLease(lease)
	svc.goalProbeByCommand = map[string]string{"probe": goalInterlockKey("inst-1", "thread-main")}
	surface.QueuedQueueItemIDs = []string{"queue-1"}
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{ID: "queue-1", SurfaceSessionID: surface.SurfaceSessionID, Status: state.QueueItemQueued}
	if _, ok := svc.HandlePrivateOwnershipQueryFailure("other", "inst-1", "probe", nil); ok {
		t.Fatal("foreign surface settled probe")
	}
	out, ok := svc.HandlePrivateOwnershipQueryFailure(surface.SurfaceSessionID, "inst-1", "probe", nil)
	if !ok || len(out) == 0 || lease.Phase != GoalInterlockOwnershipLost || lease.ProbeCommandID != "" || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("lost goal input or proof: %+v", lease)
	}
	if events := svc.ReconcileGoalInterlocks(); len(events) != 0 {
		t.Fatalf("ownership-lost probe replayed: %+v", events)
	}
	cp, err := svc.ExportPrivateCheckpoint(surface.SurfaceSessionID)
	if err != nil {
		t.Fatal(err)
	}
	cp.NextQueueID = 1
	cp.GatewayID, cp.ChatID, cp.ActorUserID = "gateway", "chat", "user"
	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	var saved PrivateCheckpoint
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	restored := newServiceForTest(&now)
	restored.config.PrivateApplication = true
	if err = restored.RestorePrivateCheckpoint(saved); err != nil {
		t.Fatal(err)
	}
	if events := restored.ReconcileGoalInterlocks(); len(events) != 0 {
		t.Fatal("restart replayed ownership-lost probe")
	}
}
