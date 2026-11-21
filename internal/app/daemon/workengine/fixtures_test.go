package workengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func transaction(t *testing.T, s *workstore.Store, fn func(*workstore.Tx) error) {
	t.Helper()
	if err := s.Within(t.Context(), fn); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, scopes ...string) (*workstore.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "work.sqlite")
	s, err := workstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	transaction(t, s, func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		for _, role := range []string{"leader", "expert"} {
			if err := tx.PutRole(work.Role{ID: role, Name: role, Description: "worker", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
				return err
			}
			if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: role, GatewayRef: "gateway", AppID: role, IdentityGeneration: 1}, 0); err != nil {
				return err
			}
		}
		if err := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		for _, role := range []string{"leader", "expert"} {
			if err := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: role, Active: true, Revision: 1}, 0); err != nil {
				return err
			}
		}
		for _, scope := range scopes {
			if err := tx.PutScope(work.WorkScope{ID: scope, Kind: work.ScopeProject, ProjectID: "project-" + scope, TeamID: "team", Revision: 1}, 0); err != nil {
				return err
			}
			if err := tx.PutProject(work.Project{ID: "project-" + scope, Name: scope, ScopeID: scope, TeamID: "team", LeaderRoleID: "leader", State: work.ProjectActive, Revision: 1}, 0); err != nil {
				return err
			}
			for _, role := range []string{"leader", "expert"} {
				if err := tx.PutProjectMember(work.ProjectMember{ProjectID: "project-" + scope, RoleID: role, State: work.ProjectMemberReady, Revision: 1}, 0); err != nil {
					return err
				}
				if err := putWorker(tx, scope, role, "", ""); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return s, path
}
func putWorker(tx *workstore.Tx, scope, role, taskID, suffix string) error {
	id := scope + ":" + role + suffix
	context := work.WorkContext{ID: "context:" + id, ScopeID: scope, RoleID: role, TaskID: taskID, Workstream: "stream:" + id, Revision: 1}
	if err := tx.PutContext(context, 0); err != nil {
		return err
	}
	return tx.PutWorker(work.WorkerSession{ID: "worker:" + id, ContextID: context.ID, ScopeID: scope, RoleID: role, Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, InstanceID: "instance:" + id, InstanceGeneration: 1, ProcessGeneration: 1, CarrierIncarnation: "carrier:" + id, Controller: work.ControllerAutomatic, ControllerGeneration: 1, Revision: 1}, 0)
}
func enqueue(t *testing.T, s *workstore.Store, id, scope, role, task, contextID string) {
	t.Helper()
	transaction(t, s, func(tx *workstore.Tx) error {
		return tx.PutInbox(work.Inbox{ID: id, ScopeID: scope, RecipientRoleID: role, RecipientContextID: contextID, TaskID: task, EventID: "event:" + id, State: work.InboxPending, AvailableAt: testNow, Revision: 1}, 0)
	})
}

type testPlanner struct {
	sequence atomic.Int64
	hook     func(Candidate)
}

func (p *testPlanner) Plan(_ context.Context, c Candidate) (DispatchPlan, error) {
	if p.hook != nil {
		p.hook(c)
	}
	id := fmt.Sprint(p.sequence.Add(1))
	return DispatchPlan{WorkerRevision: c.Worker.Revision, InstructionRevision: "instructions:1", Command: agentproto.Command{CommandID: "command:" + id, Kind: agentproto.CommandPromptSend, Execution: &agentproto.ExecutionBinding{Version: agentproto.WorkerProtocolVersion, ScopeID: c.Scope.ID, ContextID: c.Context.ID, RoleID: c.Worker.RoleID, WorkerSessionID: c.Worker.ID, SessionGeneration: c.Worker.Generation, ControllerGeneration: c.Worker.ControllerGeneration, BotGeneration: c.BotBinding.IdentityGeneration, AttemptID: "attempt:" + id, RunRef: "run:" + id}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "work"}}}}}, nil
}

type sentCommand struct {
	instance string
	command  agentproto.Command
}
type testDispatcher struct {
	mu    sync.Mutex
	calls []sentCommand
	send  func(string, agentproto.Command) (DispatchReceipt, error)
}

func (d *testDispatcher) Send(_ context.Context, instance string, command agentproto.Command) (DispatchReceipt, error) {
	d.mu.Lock()
	d.calls = append(d.calls, sentCommand{instance, command})
	d.mu.Unlock()
	if d.send != nil {
		return d.send(instance, command)
	}
	return DispatchReceipt{State: DispatchDelivered, TransportReceipt: "transport:" + command.CommandID}, nil
}
func (d *testDispatcher) count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.calls) }
func engine(t *testing.T, s *workstore.Store, slots int, p Planner, d Dispatcher) *Engine {
	t.Helper()
	e, err := New(s, Config{MaxConcurrent: slots, Now: func() time.Time { return testNow }}, p, d)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func finish(t *testing.T, s *workstore.Store, id string) {
	t.Helper()
	transaction(t, s, func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt(id)
		if err != nil {
			return err
		}
		expected := attempt.Revision
		attempt.State = work.AttemptRunning
		attempt.Revision++
		if err = tx.PutAttempt(attempt, expected); err != nil {
			return err
		}
		body := work.NewContent("outcome:"+id, attempt.ScopeID, "text/plain", []byte("done"), testNow)
		if err = tx.PutContent(body); err != nil {
			return err
		}
		return tx.CommitOutcome(workstore.OutcomeRequest{AttemptID: id, RunRef: attempt.RunRef, ExpectedAttemptRevision: attempt.Revision, ProcessedInputIDs: attempt.InputIDs, OutcomeRef: body.ID, TerminalState: work.AttemptCompleted, TerminalEvidence: []work.SourceRef{{Kind: "runtime", ID: id, Version: "1", EvidenceRef: body.ID}}, Quiescent: true, Now: testNow})
	})
}
func journalReceipt(command agentproto.Command, carrier string) DispatchReceipt {
	raw, _ := json.Marshal(command)
	sum := sha256.Sum256(raw)
	return DispatchReceipt{State: DispatchDelivered, Journal: &agentproto.RuntimeCommandReceipt{Version: agentproto.JournalProtocolVersion, CarrierIncarnation: carrier, CommandID: command.CommandID, PayloadHash: hex.EncodeToString(sum[:]), Stage: "prepared"}}
}
