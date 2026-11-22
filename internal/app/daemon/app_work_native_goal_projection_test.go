package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func markPrivateNativeViewPublished(t *testing.T, a *App, id string) {
	t.Helper()
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(id)
		if err != nil {
			return err
		}
		view.ExternalMessageID = "native-card:" + id
		view.Revision++
		if err = tx.PutUIView(view, view.Revision-1); err != nil {
			return err
		}
		op, err := tx.GetOperation(view.OperationID)
		if err != nil {
			return err
		}
		before := op.Revision
		op.Revision++
		op.State = work.OperationSending
		if _, _, err = tx.PutOperation(op, before); err != nil {
			return err
		}
		before = op.Revision
		op.Revision++
		op.State = work.OperationConfirmed
		op.ExternalReceipt = view.ExternalMessageID
		_, _, err = tx.PutOperation(op, before)
		return err
	})
}

func (f *privateMCPReadFixture) goalReply(t *testing.T, command agentproto.Command, objective string) {
	t.Helper()
	f.sequence++
	budget := int64(1200)
	event := agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command.CommandID, RequestID: "goal-rpc:" + command.CommandID, Execution: agentproto.CloneExecutionBinding(command.Execution), ThreadID: command.Target.ThreadID, ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: command.Target.ThreadID, Objective: objective, Status: "active", TokenBudget: &budget, TokensUsed: 87, CreatedAt: 10, UpdatedAt: 20}, Seq: f.sequence}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: f.carrier.Incarnation, Sequence: f.sequence, PayloadHash: hash}
	if err = f.coordinator.HandleEvent(t.Context(), f.carrier.OriginInstanceID, event); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateGoalExactNativeReplyAutomaticallyUpdatesOriginalCard(t *testing.T) {
	f := newPrivateMCPReadFixture(t)
	transport := f.relay.send
	f.relay.send = func(instance string, command agentproto.Command) error {
		pickerTx(t, f.a, func(tx *workstore.Tx) error {
			op, err := tx.GetOperation(command.CommandID)
			if err != nil {
				return err
			}
			for _, step := range op.Steps {
				if step.ID == "ui_pending" {
					_, err = tx.GetUIView(step.Receipt)
					return err
				}
			}
			t.Fatal("native Goal was sent before durable pending UI ownership")
			return nil
		})
		return transport(instance, command)
	}
	pending, err := f.a.teamNativeGoal(t.Context(), f.source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal"})
	if err != nil {
		t.Fatal(err)
	}
	markPrivateNativeViewPublished(t, f.a, pending)
	if len(f.commands) != 1 || f.commands[0].Kind != agentproto.CommandThreadGoalGet {
		t.Fatal("Goal read lost owner-only command")
	}
	f.goalReply(t, f.commands[0], "actual-session-objective")
	if err = f.a.advanceTeamGoalUI(t.Context(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	id := workui.ID(f.commands[0].CommandID, "result")
	card := goalUICardBody(t, f.a, f.source.ScopeID, id)
	if !strings.Contains(card, "actual-session-objective") || !strings.Contains(card, "1200") || !strings.Contains(card, "87") {
		t.Fatalf("actual native Goal result missing: %s", card)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(id)
		if err != nil {
			return err
		}
		if view.ReplacesViewID != pending {
			t.Fatal("Goal RPC result created unrelated card")
		}
		op, err := tx.GetOperation(f.commands[0].CommandID)
		if err != nil {
			return err
		}
		for _, step := range op.Steps {
			if step.ID == "ui_result" {
				return nil
			}
		}
		t.Fatal("confirmed Goal UI wasn't durably settled")
		return nil
	})
}

func TestPrivateGoalOldReplyCannotOverwriteNewGoalPage(t *testing.T) {
	f := newPrivateMCPReadFixture(t)
	first, err := f.a.teamNativeGoal(t.Context(), f.source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal"})
	if err != nil {
		t.Fatal(err)
	}
	markPrivateNativeViewPublished(t, f.a, first)
	newerSource := f.source
	newerSource.Key = "newer-goal-query"
	second, err := f.a.teamNativeGoal(t.Context(), newerSource, control.Action{Kind: control.ActionGoalCommand, Text: "/goal"})
	if err != nil {
		t.Fatal(err)
	}
	markPrivateNativeViewPublished(t, f.a, second)
	f.goalReply(t, f.commands[0], "old-goal-response")
	if err = f.a.advanceTeamGoalUI(t.Context(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		if _, err := tx.GetUIView(workui.ID(f.commands[0].CommandID, "result")); err != workstore.ErrNotFound {
			t.Fatal("old Goal response created a new result card")
		}
		view, err := tx.GetUIView(second)
		if err == nil && view.Invalidated {
			t.Fatal("old Goal receipt invalidated newer query page")
		}
		return err
	})
}

func TestPrivateGoalPendingMetadataRepairsOnSourceReplayWithoutResending(t *testing.T) {
	f := newPrivateMCPReadFixture(t)
	pending, err := f.a.teamNativeGoal(t.Context(), f.source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal"})
	if err != nil {
		t.Fatal(err)
	}
	id := f.commands[0].CommandID
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		steps := []work.OperationStep{}
		for _, step := range op.Steps {
			if step.ID != "ui_pending" && step.ID != "carrier" && step.ID != "dispatch_started" {
				steps = append(steps, step)
			}
		}
		before := op.Revision
		op.Steps = steps
		op.Revision++
		_, _, err = tx.PutOperation(op, before)
		return err
	})
	replayed, err := f.a.teamNativeGoal(t.Context(), f.source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal"})
	if err != nil {
		t.Fatal(err)
	}
	if replayed != pending || len(f.commands) != 1 {
		t.Fatal("source replay duplicated native mutation or pending card")
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		for _, step := range op.Steps {
			if step.ID == "ui_pending" && step.Receipt == pending {
				return nil
			}
		}
		t.Fatal("source replay failed to repair missing UI registration")
		return nil
	})
}

func TestPrivateGoalUnknownResultDoesNotResendNativeCommand(t *testing.T) {
	f := newPrivateMCPReadFixture(t)
	pending, err := f.a.teamNativeGoal(t.Context(), f.source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal"})
	if err != nil {
		t.Fatal(err)
	}
	markPrivateNativeViewPublished(t, f.a, pending)
	if err = f.a.advanceTeamGoalUI(t.Context(), time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 {
		t.Fatal("Goal timeout automatically replayed a native command")
	}
	card := goalUICardBody(t, f.a, f.source.ScopeID, workui.ID(f.commands[0].CommandID, "result"))
	if !strings.Contains(card, "结果暂未确认") || !strings.Contains(card, "不会自动重发") {
		t.Fatalf("unknown Goal result claimed success: %s", card)
	}
}

func TestPrivateGoalPendingRegistrationFailureRollsBackCardAndOutbox(t *testing.T) {
	for _, kind := range []string{"native-command", "goal-intent"} {
		t.Run(kind, func(t *testing.T) {
			a, source := teamPickerFixture(t)
			var err error
			if kind == "native-command" {
				_, err = a.queueTeamGoalCommandPending(t.Context(), source, "missing-native-operation")
			} else {
				_, err = a.queueTeamGoalIntentPending(t.Context(), source, "missing-goal-intent")
			}
			if err == nil {
				t.Fatal("missing persisted Goal source accepted")
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				if _, err := tx.GetUIView(workui.ID(source.Key, "session-goal", "overview")); err != workstore.ErrNotFound {
					t.Fatal("failed Goal registration left orphan pending card")
				}
				outbox, err := tx.ListOutbox(source.ScopeID)
				if len(outbox) != 0 {
					t.Fatal("failed Goal registration left orphan publication")
				}
				return err
			})
		})
	}
}

func TestPrivateCancelledGoalIntentSettlesPendingPresentation(t *testing.T) {
	a, source := teamPickerFixture(t)
	const intentID = "cancelled-goal-intent"
	pickerTx(t, a, func(tx *workstore.Tx) error {
		nav, err := tx.GetPrivateNavigation(source.ScopeID)
		if err != nil {
			return err
		}
		budget := int64(1200)
		_, _, err = tx.QueueSessionGoalIntent(intentID, nav.FrontContextID, agentproto.GoalCommand{Objective: "explicit session work", TokenBudget: &budget, Purpose: "user_control"}, source.Authority, time.Now().UTC())
		return err
	})
	pending, err := a.queueTeamGoalIntentPending(t.Context(), source, intentID)
	if err != nil {
		t.Fatal(err)
	}
	markPrivateNativeViewPublished(t, a, pending)
	pickerTx(t, a, func(tx *workstore.Tx) error {
		intent, err := tx.GetSessionGoalIntent(intentID)
		if err != nil {
			return err
		}
		input, err := tx.GetInbox(intent.InputID)
		if err != nil {
			return err
		}
		previous := input.Revision
		input.Revision++
		input.State = work.InboxCancelled
		return tx.PutInbox(input, previous)
	})
	if err = a.advanceTeamGoalUI(t.Context(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	card := goalUICardBody(t, a, source.ScopeID, workui.ID(intentID+":ui", "result"))
	if !strings.Contains(card, "排队请求已取消") {
		t.Fatalf("cancelled Goal intent still looked pending: %s", card)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(intentID + ":ui")
		if err != nil {
			return err
		}
		if op.State != work.OperationTerminal {
			t.Fatal("cancelled Goal UI wrapper retained business occupancy")
		}
		return nil
	})
}
