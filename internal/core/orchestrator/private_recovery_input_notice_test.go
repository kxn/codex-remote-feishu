package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func recoveryWaitingNotice(events []eventcontract.Event) *control.TimelineText {
	for _, event := range events {
		if event.TimelineText != nil && event.TimelineText.Type == control.TimelineTextRecoveryInputWaiting {
			return event.TimelineText
		}
	}
	return nil
}

func TestPrivateRecoveryInputNoticeAndQueueReasonSurviveRestart(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "new-ready"}[fresh], func(t *testing.T) {
			s, req := privateUpgradeResumeFixture(t, fresh)
			v := s.Surface("s")
			v.QueuedQueueItemIDs = nil
			v.QueueItems = map[string]*state.QueueItemRecord{}
			v.StagedImages = nil
			events, err := s.PreparePrivateRuntimeReplacement(req)
			if err != nil || len(events) != 1 || events[0].DaemonCommand == nil {
				t.Fatalf("prepare: %v", err)
			}
			v.PendingHeadless.AutoRestore = true
			newID := v.PendingHeadless.InstanceID
			if recoveryWaitingNotice(events) != nil {
				t.Fatal("idle recovery sent a message")
			}
			input := func(id string) control.Action {
				return control.Action{Kind: control.ActionTextMessage, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user", MessageID: id, Text: id}
			}
			first := s.ApplySurfaceAction(input("first"))
			notice := recoveryWaitingNotice(first)
			if notice == nil || notice.ReplyToMessageID != "first" || notice.Text != "后台正在恢复，已收到这条消息，恢复完成后会自动执行。" {
				t.Fatalf("wrong first notice: %+v", notice)
			}
			if s.Surface("s").QueueItems[v.QueuedQueueItemIDs[0]].WaitReason != privateCarrierRecoveryWait {
				t.Fatal("missing recovery wait reason")
			}
			second := s.ApplySurfaceAction(input("second"))
			if recoveryWaitingNotice(second) != nil || findPromptSendCommand(second) != nil {
				t.Fatal("duplicate notice or early dispatch")
			}
			if !fresh {
				if len(v.QueuedQueueItemIDs) != 2 {
					t.Fatalf("second input was not queued: %+v", second)
				}
				if s.Surface("s").QueueItems[v.QueuedQueueItemIDs[1]].WaitReason != "" {
					t.Fatal("genuinely queued second input mislabeled")
				}
			}
			cp, err := s.ExportPrivateCheckpoint("s")
			if err != nil {
				t.Fatal(err)
			}
			r := NewService(nil, Config{PrivateApplication: true}, nil)
			if err = r.RestorePrivateCheckpoint(cp); err != nil {
				t.Fatal(err)
			}
			if err = r.RestorePrivateUpgradeHeadlessProjection(req, newID); err != nil {
				t.Fatal(err)
			}
			if !r.Surface("s").PendingHeadless.RecoveryInputNoticeSent || r.Surface("s").QueueItems[r.Surface("s").QueuedQueueItemIDs[0]].WaitReason != privateCarrierRecoveryWait {
				t.Fatal("restart lost recovery facts")
			}
			if recoveryWaitingNotice(r.ApplySurfaceAction(input("third"))) != nil {
				t.Fatal("restart repeated recovery notice")
			}
			r.UpsertInstance(&state.InstanceRecord{InstanceID: newID, Source: "headless", Managed: true, Backend: req.Backend, CodexProfileID: req.ProfileID, WorkspaceRoot: req.CWD, WorkspaceKey: req.CWD, Threads: map[string]*state.ThreadRecord{}})
			connected := r.ApplyInstanceConnected(newID)
			if findQueuedStartNotice(connected) != nil {
				t.Fatal("recovery-only input got a queue-start notice")
			}
			reconciled, err := r.ReconcilePrivateRecoveryGate("s", true)
			if err != nil {
				t.Fatal(err)
			}
			all := append(connected, reconciled...)
			if findQueuedStartNotice(all) != nil {
				t.Fatal("recovery-only input got a queue-start notice")
			}
			if cmd := findPromptSendCommand(all); cmd == nil || cmd.Origin.MessageID != "first" {
				t.Fatalf("first input not dispatched: %+v", all)
			}
			if !fresh {
				r.ApplyAgentEvent(newID, agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: req.ThreadID, TurnID: "turn-1"})
				next := r.ApplyAgentEvent(newID, agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: req.ThreadID, TurnID: "turn-1", Status: "completed"})
				if n := findQueuedStartNotice(next); n == nil || n.SourceMessageID != "second" {
					t.Fatalf("busy queue lost its start notice: %+v", next)
				}
			}
		})
	}
}

func TestPrivateRecoveryNoticeIsNotRepeatedAfterInputRecall(t *testing.T) {
	s, req := privateUpgradeResumeFixture(t, false)
	v := s.Surface("s")
	v.QueuedQueueItemIDs = nil
	v.QueueItems = map[string]*state.QueueItemRecord{}
	v.StagedImages = nil
	if _, err := s.PreparePrivateRuntimeReplacement(req); err != nil {
		t.Fatal(err)
	}
	v.PendingHeadless.AutoRestore = true
	input := func(id string) control.Action {
		return control.Action{Kind: control.ActionTextMessage, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user", MessageID: id, Text: id}
	}
	if recoveryWaitingNotice(s.ApplySurfaceAction(input("first"))) == nil {
		t.Fatal("first notice missing")
	}
	s.ApplySurfaceAction(control.Action{Kind: control.ActionMessageRecalled, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user", TargetMessageID: "first"})
	if len(v.QueuedQueueItemIDs) != 0 {
		t.Fatal("recall failed to discard first input")
	}
	if recoveryWaitingNotice(s.ApplySurfaceAction(input("second"))) != nil {
		t.Fatal("same recovery sent another notice")
	}
	if len(v.QueuedQueueItemIDs) != 1 || v.QueueItems[v.QueuedQueueItemIDs[0]].WaitReason != privateCarrierRecoveryWait {
		t.Fatal("replacement input no longer records recovery-only waiting")
	}
}
