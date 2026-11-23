package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPredictionDraftSurvivesRelayFailure(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议内容"))
	svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "用户编辑后的消息")
	send := findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend)
	if send == nil {
		t.Fatal("fixture requires ordinary prompt dispatch")
	}
	send.CommandID = "review-draft-send"
	svc.BindPendingRemoteCommand(sid, send.CommandID)
	failed := svc.HandleCommandDispatchFailure(sid, send.CommandID, agentproto.ErrorInfo{Message: "relay failed"})
	if findAgentCommand(failed, agentproto.CommandPromptSend) != nil {
		t.Fatal("failure must not automatically resend the draft")
	}
	page := findPageEvent(failed)
	if page == nil || page.MessageID != action.MessageID || page.Sections[0].Entries[0].Form.Field.DefaultValue != action.Text {
		t.Fatal("failure must restore edited text to the original card")
	}
	if findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend) == nil {
		t.Fatal("relay failure consumed the editable draft; same form cannot retry edited message")
	}
}

func TestPredictionQueuedDraftCannotSubmitTwice(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议内容"))
	svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	surface := svc.root.Surfaces[sid]
	surface.DispatchMode = state.DispatchModePausedForLocal
	action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "排队的编辑内容")
	if findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend) != nil {
		t.Fatal("paused surface must queue")
	}
	if findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend) != nil || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatal("duplicate submit must not add another queue item")
	}
	svc.expirePredictions()
	surface.DispatchMode = state.DispatchModeNormal
	send := findAgentCommand(svc.dispatchNext(surface), agentproto.CommandPromptSend)
	if send == nil {
		t.Fatal("queued draft must dispatch after resume")
	}
	send.CommandID = "queued-draft-send"
	svc.BindPendingRemoteCommand(sid, send.CommandID)
	failed := svc.HandleCommandDispatchFailure(sid, send.CommandID, agentproto.ErrorInfo{Message: "relay failed"})
	if findPageEvent(failed) == nil {
		t.Fatal("queued draft must retain its retry association across ticks")
	}
	if findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend) == nil {
		t.Fatal("user must be able to retry failed queued draft")
	}
}

func TestPredictionRejectedCommandRestoresEditedDraft(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议内容"))
	svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "本地拒绝后保留的编辑")
	send := findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend)
	send.CommandID = "rejected-draft-send"
	svc.BindPendingRemoteCommand(sid, send.CommandID)
	failed := svc.HandleCommandRejected(iid, agentproto.CommandAck{CommandID: send.CommandID})
	page := findPageEvent(failed)
	if page == nil || page.Sections[0].Entries[0].Form.Field.DefaultValue != action.Text {
		t.Fatal("explicit local rejection must restore edited draft")
	}
	if findAgentCommand(failed, agentproto.CommandPromptSend) != nil {
		t.Fatal("rejection must not automatically resend")
	}
	if findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend) == nil {
		t.Fatal("user must be able to retry rejected draft")
	}
}

func TestPredictionOtherQueueFailureDoesNotRestoreDraft(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议内容"))
	svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	surface := svc.root.Surfaces[sid]
	surface.DispatchMode = state.DispatchModePausedForLocal
	action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "尚未发送的草稿")
	svc.ApplySurfaceAction(action)
	svc.enqueueQueueItem(surface, "other-message", "其他队列内容", nil, []agentproto.Input{{Type: agentproto.InputText, Text: "其他队列内容"}}, "thread-1", "/workspace", state.RouteModePinned, surface.PromptOverride, true)
	surface.DispatchMode = state.DispatchModeNormal
	other := findAgentCommand(svc.dispatchNext(surface), agentproto.CommandPromptSend)
	if other == nil || other.Prompt.Inputs[0].Text != "其他队列内容" {
		t.Fatal("fixture must dispatch other queue first")
	}
	other.CommandID = "other-queue-send"
	svc.BindPendingRemoteCommand(sid, other.CommandID)
	failed := svc.HandleCommandDispatchFailure(sid, other.CommandID, agentproto.ErrorInfo{Message: "other failed"})
	if findPageEvent(failed) != nil {
		t.Fatal("other queue failure must not restore this draft")
	}
	queued := findAgentCommand(failed, agentproto.CommandPromptSend)
	if queued == nil || queued.Prompt.Inputs[0].Text != action.Text {
		t.Fatal("ordinary queue should dispatch the original queued item once")
	}
	queued.CommandID = "actual-draft-send"
	svc.BindPendingRemoteCommand(sid, queued.CommandID)
	if findPageEvent(svc.HandleCommandDispatchFailure(sid, queued.CommandID, agentproto.ErrorInfo{Message: "draft failed"})) == nil {
		t.Fatal("draft must only restore on its own actual command failure")
	}
}

func TestPredictionRejectedSendRetainsEditedDraft(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议内容"))
	svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	inst := svc.root.Instances[iid]
	inst.WorkspaceRoot, inst.Threads["thread-1"].CWD = "", ""
	action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "验证失败也保留的编辑")
	rejected := svc.ApplySurfaceAction(action)
	if findAgentCommand(rejected, agentproto.CommandPromptSend) != nil {
		t.Fatal("missing cwd must reject ordinary sending")
	}
	page := findPageEvent(rejected)
	if page == nil || page.Sections[0].Entries[0].Form.Field.DefaultValue != action.Text {
		t.Fatal("rejected ordinary send must retain edited draft")
	}
	inst.WorkspaceRoot, inst.Threads["thread-1"].CWD = "/workspace", "/workspace"
	if findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend) == nil {
		t.Fatal("rejected draft must be retryable after validation is fixed")
	}
}

func TestPredictionFailureDoesNotReviveInvalidatedDraft(t *testing.T) {
	for _, change := range []string{"input", "route", "invalidated", "other-command"} {
		t.Run(change, func(t *testing.T) {
			svc, sid, iid := predictionTestSetup(t)
			request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
			svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议内容"))
			svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
			action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "旧编辑内容")
			send := findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend)
			send.CommandID = "old-draft-send"
			svc.BindPendingRemoteCommand(sid, send.CommandID)
			failureID := send.CommandID
			switch change {
			case "input":
				svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1", MessageID: "new-input", Text: "用户的新输入"})
			case "route":
				svc.root.Surfaces[sid].SelectedThreadID = "another-thread"
			case "invalidated":
				svc.invalidatePrediction(svc.predictions[sid])
			case "other-command":
				failureID = "unrelated-command"
			}
			failed := svc.HandleCommandDispatchFailure(sid, failureID, agentproto.ErrorInfo{Message: "relay failed"})
			if findPageEvent(failed) != nil {
				t.Fatal("unrelated or invalidated draft must not be restored")
			}
			if cmd := findAgentCommand(failed, agentproto.CommandPromptSend); cmd != nil && cmd.Prompt.Inputs[0].Text == action.Text {
				t.Fatal("failure must not automatically retry old draft")
			}
		})
	}
}

func TestPredictionRejectsNativeActiveRuntimeWithoutStartNotification(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, agentproto.Event{Kind: agentproto.EventThreadRuntimeStatusUpdated, ThreadID: "thread-1", RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeActive}})
	if findPageEvent(svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "旧轮次建议"))) != nil {
		t.Fatal("native thread is already active but old prediction result was rendered")
	}
}

func TestPredictionRejectsResultAfterDegradedReconnect(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyInstanceTransportDegraded(iid, false)
	svc.ApplyInstanceConnected(iid)
	if findPageEvent(svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "断线前建议"))) != nil {
		t.Fatal("transport degradation did not invalidate old sample before reconnect")
	}
}

func TestPredictionRejectsNativeActivityAfterReturningIdle(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	for _, kind := range []agentproto.ThreadRuntimeStatusType{agentproto.ThreadRuntimeStatusTypeActive, agentproto.ThreadRuntimeStatusTypeIdle} {
		svc.ApplyAgentEvent(iid, agentproto.Event{Kind: agentproto.EventThreadRuntimeStatusUpdated, ThreadID: "thread-1", RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: kind}})
	}
	if findPageEvent(svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "原生新活动前的旧建议"))) != nil {
		t.Fatal("native activity must permanently invalidate old sample even after idle returns")
	}
}

func TestPredictionSkipsNativeChildSourceWithoutForkMarker(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	svc.root.Instances[iid].Threads["thread-1"].Source = &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindThreadSpawn, ParentThreadID: "parent-native"}
	if findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind) != nil {
		t.Fatal("native child source without ForkedFromID must not sample as a user root")
	}
}

func TestPredictionRootSourceAdmission(t *testing.T) {
	for _, kind := range []agentproto.ThreadSourceKind{agentproto.ThreadSourceKindUser, agentproto.ThreadSourceKindAppServer, agentproto.ThreadSourceKindCustom, agentproto.ThreadSourceKindUnknown, agentproto.ThreadSourceKindCompact, agentproto.ThreadSourceKindThreadSpawn, agentproto.ThreadSourceKindMemoryConsolidation, agentproto.ThreadSourceKindSubAgentOther} {
		t.Run(string(kind), func(t *testing.T) {
			svc, sid, iid := predictionTestSetup(t)
			svc.root.Instances[iid].Threads["thread-1"].Source = &agentproto.ThreadSourceRecord{Kind: kind}
			request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
			want := kind == agentproto.ThreadSourceKindUser || kind == agentproto.ThreadSourceKindAppServer || kind == agentproto.ThreadSourceKindCustom || kind == agentproto.ThreadSourceKindUnknown
			if (request != nil) != want {
				t.Fatalf("source %s prediction admission: got %t, want %t", kind, request != nil, want)
			}
		})
	}
}

func TestPredictionRejectsNativeActiveSnapshot(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.root.Instances[iid].Threads["thread-1"].RuntimeStatus = &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeActive}
	if findPageEvent(svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "旧建议"))) != nil {
		t.Fatal("active snapshot must reject stale result without a start event")
	}
}

func TestPredictionUnsupportedRetriesAfterDegradedReconnect(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	unsupported := predictionTestResult(t, request.CommandID, "turn-1", "")
	unsupported.Prediction.Unsupported = true
	svc.ApplyAgentEvent(iid, unsupported)
	if !svc.predictionUnsupported[iid] {
		t.Fatal("fixture must disable current connection")
	}
	svc.ApplyInstanceTransportDegraded(iid, false)
	svc.ApplyInstanceConnected(iid)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1", MessageID: "second-input", Text: "重连后继续"})
	initiator := agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: sid}
	svc.ApplyAgentEvent(iid, agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-2", Initiator: initiator})
	completed := svc.ApplyAgentEvent(iid, agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", TurnID: "turn-2", Status: "completed", Initiator: initiator})
	if findAgentCommand(completed, predictionRequestKind) == nil {
		t.Fatal("new connection must retry capability after prior method unsupported")
	}
}
