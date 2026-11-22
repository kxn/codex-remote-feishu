package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func goalUICardBody(t *testing.T, a *App, scopeID, id string) string {
	t.Helper()
	var card string
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(id)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(scopeID, view.BodyRef)
		card = string(body.Data)
		return err
	})
	return card
}

func TestPrivateGoalInteractiveSlashEntrypointsWithoutNativeSession(t *testing.T) {
	for _, tt := range []struct{ text, contains string }{
		{"/goal", "当前还没有可查询的原生会话"},
		{"/goal status", "private.goal-new"},
		{"/goal new", "private.goal-new-submit"},
		{"/goal edit", "private.goal-edit-submit"},
		{"/goal clear", "当前没有已确认可清除的目标"},
	} {
		t.Run(tt.text, func(t *testing.T) {
			a, source := teamPickerFixture(t)
			id, err := a.teamNativeGoal(t.Context(), source, control.Action{Kind: control.ActionGoalCommand, Text: tt.text})
			if err != nil {
				t.Fatal(err)
			}
			if card := goalUICardBody(t, a, source.ScopeID, id); !strings.Contains(card, tt.contains) {
				t.Fatalf("missing goal entry %q: %s", tt.contains, card)
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				workers, err := tx.ListWorkers(source.ScopeID)
				if len(workers) != 0 {
					t.Fatal("read-only goal entry started a native session")
				}
				inputs, err := tx.ListInbox(source.ScopeID)
				if len(inputs) != 0 {
					t.Fatal("goal form created business input before submission")
				}
				return err
			})
		})
	}
}

func TestPrivateGoalShowsConfirmedNativeResultForCurrentSessionOnly(t *testing.T) {
	a, source := teamPickerFixture(t)
	pickerTx(t, a, func(tx *workstore.Tx) error {
		nav, err := tx.GetPrivateNavigation(source.ScopeID)
		if err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "goal-worker", ScopeID: source.ScopeID, ContextID: nav.FrontContextID, RoleID: source.RoleID, Backend: work.BackendCodex, State: work.WorkerReady, Controller: work.ControllerAutomatic, Generation: 2, ControllerGeneration: 1, NativeSessionID: "actual-session", NativeNamespace: "codex", Revision: 1}
		if err = tx.PutWorker(worker, 0); err != nil {
			return err
		}
		budget := int64(1200)
		for i, result := range []struct {
			id, session, objective string
			generation             int64
		}{
			{"current", "actual-session", "实际会话目标", 2},
			{"other-session", "wrong-session", "错误会话目标", 2},
			{"old-worker", "actual-session", "旧执行目标", 1},
		} {
			now := time.Now().UTC().Add(time.Duration(i) * time.Second)
			raw, _ := json.Marshal(agentproto.Event{ThreadID: result.session, ThreadGoal: &agentproto.ThreadGoalUpdate{Objective: result.objective, Status: "active", TokenBudget: &budget}})
			body := work.NewContent(result.id+":receipt", source.ScopeID, "application/json", raw, now)
			if err = tx.PutContent(body); err != nil {
				return err
			}
			_, _, err = tx.PutOperation(work.Operation{ID: result.id, ScopeID: source.ScopeID, ContextID: worker.ContextID, Kind: "native.user.goal", TargetID: worker.ID, TargetGeneration: result.generation, Actor: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, RequestHash: "fixture", IdempotencyKey: result.id, State: work.OperationConfirmed, ExternalReceipt: body.ID, Revision: 1, CreatedAt: now, UpdatedAt: now}, 0)
			if err != nil {
				return err
			}
		}
		return nil
	})
	id, err := a.queueTeamGoalPanel(t.Context(), source, "overview", "")
	if err != nil {
		t.Fatal(err)
	}
	card := goalUICardBody(t, a, source.ScopeID, id)
	if !strings.Contains(card, "实际会话目标") || !strings.Contains(card, "1200") || strings.Contains(card, "错误会话目标") || strings.Contains(card, "旧执行目标") {
		t.Fatalf("goal result lost exact session identity: %s", card)
	}
}

func TestPrivateGoalParserRetainsConfirmedClearAndRejectsDroppedArguments(t *testing.T) {
	if kind, _, err := parseTeamGoal("/goal clear --confirm"); err != nil || kind != agentproto.CommandThreadGoalClear {
		t.Fatalf("legacy clear confirm: %s %v", kind, err)
	}
	for _, text := range []string{"/goal status ignored", "/goal set --budget= objective", "/goal clear ignored", "/goal pause ignored"} {
		if _, _, err := parseTeamGoal(text); err == nil {
			t.Fatalf("silently discarded arguments in %q", text)
		}
	}
}

func TestPrivateGoalConfirmationRejectsChangedSessionOnSameNavigation(t *testing.T) {
	f := newPrivateMCPReadFixture(t)
	a, source := f.a, f.source
	if _, err := a.teamNativeGoal(t.Context(), source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal"}); err != nil {
		t.Fatal(err)
	}
	f.goalReply(t, f.commands[0], "confirmed goal")
	source.Key = "clear-current-goal"
	id, err := a.queueTeamGoalPanel(t.Context(), source, "clear", "")
	if err != nil {
		t.Fatal(err)
	}
	_, interaction := pickerAction(t, a, source, id, "private.goal-clear-confirm")
	pickerTx(t, a, func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("mcp-worker")
		if err != nil {
			return err
		}
		previous := worker.Revision
		worker.Revision++
		worker.Generation++
		worker.NativeSessionID = "new-session"
		return tx.PutWorker(worker, previous)
	})
	clicked := source
	clicked.Key = interaction.ID
	clicked.Authority = workstore.PrivateActionSource{PersonalScopeID: source.ScopeID, InteractionID: interaction.ID}
	if _, err = a.handleTeamGoalViewOperation(t.Context(), clicked, "private.goal-clear-confirm", nil); err == nil {
		t.Fatal("stale clear confirmation reached a replacement session")
	}
	if len(f.commands) != 1 {
		t.Fatal("stale confirmation sent a native clear")
	}
}

func TestPrivateGoalWithoutNativeSessionNeverOffersImpossibleClear(t *testing.T) {
	a, source := teamPickerFixture(t)
	id, err := a.teamNativeGoal(t.Context(), source, control.Action{Kind: control.ActionGoalCommand, Text: "/goal clear"})
	if err != nil {
		t.Fatal(err)
	}
	if card := goalUICardBody(t, a, source.ScopeID, id); strings.Contains(card, "private.goal-clear-confirm") {
		t.Fatalf("no-native Goal offered unusable clear: %s", card)
	}
}

func TestPrivateBareWorktreeCommandDoesNotQueueUnspecifiedWork(t *testing.T) {
	a, source := teamPickerFixture(t)
	id, err := a.handleTeamPrivateOperation(t.Context(), source, control.Action{Kind: control.ActionWorkspaceNewWorktree, Text: "/workspace new worktree"})
	if err != nil {
		t.Fatal(err)
	}
	if card := goalUICardBody(t, a, source.ScopeID, id); !strings.Contains(card, "工作树由助手") {
		t.Fatalf("missing agent-managed workspace guidance: %s", card)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		inputs, err := tx.ListInbox(source.ScopeID)
		if len(inputs) != 0 {
			t.Fatal("bare worktree command silently created work")
		}
		return err
	})
}
