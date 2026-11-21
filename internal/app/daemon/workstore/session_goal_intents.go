package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaSessionGoalIntentsV1 = `CREATE TABLE session_goal_intents(id TEXT PRIMARY KEY,scope_id TEXT NOT NULL REFERENCES scopes(id),context_id TEXT NOT NULL REFERENCES contexts(id),payload TEXT NOT NULL CHECK(json_valid(payload)));`

type SessionGoalIntent struct {
	ID        string                 `json:"id"`
	ScopeID   string                 `json:"scope_id"`
	ContextID string                 `json:"context_id"`
	Goal      agentproto.GoalCommand `json:"goal"`
	Source    work.SourceRef         `json:"source"`
	InputID   string                 `json:"input_id"`
	CommandID string                 `json:"command_id,omitempty"`
	AttemptID string                 `json:"attempt_id,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

func (t *Tx) GetSessionGoalIntent(id string) (SessionGoalIntent, error) {
	var v SessionGoalIntent
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM session_goal_intents WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
func (t *Tx) QueueSessionGoalIntent(id, contextID string, goal agentproto.GoalCommand, source PrivateActionSource, now time.Time) (v SessionGoalIntent, created bool, err error) {
	err = t.atomic(func() error {
		if id == "" || now.IsZero() || goal.Objective == "" || goal.TokenBudget == nil || *goal.TokenBudget <= 0 {
			return fmt.Errorf("goal intent needs explicit objective and positive budget")
		}
		scope, proof, e := t.ValidatePrivateAction(source)
		if e != nil {
			return e
		}
		wc, e := t.GetContext(contextID)
		if e != nil {
			return e
		}
		if wc.ScopeID != scope.ID || wc.RoleID != scope.RoleID || wc.Purpose == work.PurposeReview || wc.Purpose == work.PurposeControl {
			return ErrScopeDenied
		}
		if source.CommandID != "" {
			command, e := t.GetPrivateCommand(source.CommandID)
			if e != nil {
				return e
			}
			if command.ContextID != wc.ID {
				return ErrScopeDenied
			}
		}
		old, e := t.GetSessionGoalIntent(id)
		if e == nil {
			if old.ContextID != contextID || !reflect.DeepEqual(old.Goal, goal) || old.Source.ID != proof.ID || old.Source.Kind != proof.Kind {
				return ErrIdempotencyConflict
			}
			v = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		v = SessionGoalIntent{ID: id, ScopeID: scope.ID, ContextID: contextID, Goal: goal, Source: proof, InputID: id + ":input", CreatedAt: now}
		body := work.NewContent(id+":body", scope.ID, "text/plain", []byte("用户设定了有界会话目标，请围绕该目标执行已授权工作，不改变项目需求或验收规则。目标："+goal.Objective), now)
		if e = t.PutContent(body); e != nil {
			return e
		}
		message := work.Message{ID: id + ":message", ScopeID: scope.ID, ConversationID: wc.ID, Source: work.Actor{Kind: work.ActorSystem, ID: "user-session-goal"}, DerivedFrom: &proof, BodyRef: body.ID, Revision: 1, CreatedAt: now}
		input := work.Inbox{ID: v.InputID, ScopeID: scope.ID, RecipientRoleID: scope.RoleID, RecipientContextID: wc.ID, SessionGoalIntentID: id, MessageID: message.ID, EventID: id, State: work.InboxPending, Revision: 1, AvailableAt: now}
		// Persist the intent first so PutInbox can validate its exact ownership.
		raw, e := json.Marshal(v)
		if e != nil {
			return e
		}
		if _, e = t.tx.ExecContext(t.ctx, "INSERT INTO session_goal_intents(id,scope_id,context_id,payload) VALUES(?,?,?,?)", id, scope.ID, wc.ID, string(raw)); e != nil {
			return e
		}
		if _, _, e = t.AcceptMessage(message, []work.Inbox{input}); e != nil {
			return e
		}
		created = true
		return nil
	})
	return
}

// Called from the committed native-start receipt, never from a guessed latest
// session. Transport remains outside the transaction in the dispatch lane.
func (t *Tx) PrepareSessionGoalsForAttempt(attemptID, sessionID string, now time.Time) (commands []agentproto.Command, err error) {
	err = t.atomic(func() error {
		attempt, e := t.GetAttempt(attemptID)
		if e != nil {
			return e
		}
		worker, e := t.GetWorker(attempt.RunRef.WorkerID)
		if e != nil {
			return e
		}
		if attempt.State != work.AttemptRunning || worker.Backend != work.BackendCodex || worker.NativeSessionID != sessionID || sessionID == "" || now.IsZero() {
			return ErrConflict
		}
		if e = work.ValidateAttemptBinding(attempt, worker); e != nil {
			return e
		}
		wc, e := t.GetContext(worker.ContextID)
		if e != nil {
			return e
		}
		if wc.Purpose == work.PurposeControl || wc.Purpose == work.PurposeReview {
			return ErrScopeDenied
		}
		gates, e := t.ListPauseGates(worker.ScopeID)
		if e != nil {
			return e
		}
		allowed, e := work.GatesAllowRunForPurpose(worker.ScopeID, attempt.TaskID, wc.ID, wc.Purpose, gates)
		if e != nil {
			return e
		}
		if !allowed {
			return ErrScopeDenied
		}
		if _, e = t.GetRuntimeGate(worker.ID); e == nil {
			return ErrScopeDenied
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		carrier, e := t.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		if !carrier.Matches(worker) {
			return ErrConflict
		}
		for _, id := range attempt.InputIDs {
			input, e := t.GetInbox(id)
			if e != nil {
				return e
			}
			if input.SessionGoalIntentID == "" {
				continue
			}
			v, e := t.GetSessionGoalIntent(input.SessionGoalIntentID)
			if e != nil {
				return e
			}
			if v.InputID != id || v.ContextID != wc.ID || v.ScopeID != worker.ScopeID || input.ClaimedAttemptID != attempt.ID {
				return ErrScopeDenied
			}
			if v.CommandID != "" {
				if v.AttemptID != attempt.ID {
					return ErrConflict
				}
				continue
			}
			binding := carrier.Owner
			binding.AttemptID = attempt.ID
			binding.RunRef = attempt.RunRef.RunID
			command := agentproto.Command{CommandID: v.ID + ":command", Kind: agentproto.CommandThreadGoalSet, Execution: &binding, Target: agentproto.Target{ThreadID: sessionID}, Goal: v.Goal, IssuedAt: now}
			raw, e := json.Marshal(command)
			if e != nil {
				return e
			}
			body := work.NewContent(command.CommandID+":body", worker.ScopeID, "application/json", raw, now)
			if e = t.PutContent(body); e != nil {
				return e
			}
			scope, e := t.GetScope(worker.ScopeID)
			if e != nil {
				return e
			}
			op := work.Operation{ID: command.CommandID, ScopeID: worker.ScopeID, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Kind: "native.user.goal", TargetID: worker.ID, TargetGeneration: worker.Generation, RequestHash: body.SHA256, RequestRef: body.ID, IdempotencyKey: command.CommandID, State: work.OperationPrepared, Steps: []work.OperationStep{{ID: "human_source", State: work.OperationConfirmed, Receipt: mustJSON(v.Source)}}, Revision: 1, CreatedAt: now, UpdatedAt: now}
			canonical, created, e := t.PutOperation(op, 0)
			if e != nil {
				return e
			}
			if !created || canonical.ID != op.ID {
				return ErrIdempotencyConflict
			}
			v.CommandID = command.CommandID
			v.AttemptID = attempt.ID
			raw, e = json.Marshal(v)
			if e != nil {
				return e
			}
			if _, e = t.tx.ExecContext(t.ctx, "UPDATE session_goal_intents SET payload=? WHERE id=?", string(raw), v.ID); e != nil {
				return e
			}
			commands = append(commands, command)
		}
		return nil
	})
	return
}

func (t *Tx) ListPreparedSessionGoalOperations() ([]work.Operation, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT o.payload FROM operations o JOIN session_goal_intents i ON json_extract(i.payload,'$.command_id')=o.id WHERE o.kind='native.user.goal' AND json_extract(o.payload,'$.state')='prepared' ORDER BY o.id LIMIT 64`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []work.Operation
	for rows.Next() {
		var raw string
		var op work.Operation
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &op); err != nil {
			return nil, err
		}
		list = append(list, op)
	}
	return list, rows.Err()
}
