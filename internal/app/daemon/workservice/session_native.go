package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type NativeCommandDispatcher interface {
	SendCommand(context.Context, string, agentproto.Command) error
}

func (s *Service) WithNativeDispatcher(dispatcher NativeCommandDispatcher) *Service {
	copy := *s
	copy.nativeDispatcher = dispatcher
	return &copy
}

type SessionGoalRequest struct {
	Key         string `json:"key"`
	Action      string `json:"action"`
	Objective   string `json:"objective,omitempty"`
	TokenBudget *int64 `json:"token_budget,omitempty"`
}

// SessionGoal acts only on the authenticated current execution's Codex session.
// It never changes task requirements, acceptance or the project's business goal.
func (s *Service) SessionGoal(ctx context.Context, token, runRef string, req SessionGoalRequest) (result Result, err error) {
	var command agentproto.Command
	var instance string
	send := false
	err = s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		context, e := tx.GetContext(c.Worker.ContextID)
		if e != nil {
			return e
		}
		if context.Purpose == work.PurposeReview {
			return fmt.Errorf("read-only review cannot start native continuation")
		}
		if s.nativeDispatcher == nil {
			return fmt.Errorf("native dispatcher unavailable")
		}
		if strings.TrimSpace(req.Key) == "" || len(req.Key) > 256 || c.Worker.Backend != work.BackendCodex || c.Worker.NativeSessionID == "" {
			return fmt.Errorf("session goal requires current Codex session and key")
		}
		kind := agentproto.CommandThreadGoalGet
		switch req.Action {
		case "get":
		case "clear":
			kind = agentproto.CommandThreadGoalClear
		case "set":
			kind = agentproto.CommandThreadGoalSet
			if strings.TrimSpace(req.Objective) == "" || req.TokenBudget == nil || *req.TokenBudget <= 0 {
				return fmt.Errorf("native continuation requires explicit objective and positive token budget")
			}
		default:
			return fmt.Errorf("goal action must be get, set or clear")
		}
		if req.Action != "set" && (req.Objective != "" || req.TokenBudget != nil) {
			return fmt.Errorf("get/clear cannot silently accept a new objective")
		}
		carrier, e := tx.GetCarrier(c.Worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		if !carrier.Matches(c.Worker) {
			return workstore.ErrConflict
		}
		binding := carrier.Owner
		binding.AttemptID = c.Attempt.ID
		binding.RunRef = c.Attempt.RunRef.RunID
		id := stableID("session-goal", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		command = agentproto.Command{CommandID: id, Kind: kind, Execution: &binding, Target: agentproto.Target{ThreadID: c.Worker.NativeSessionID}, Goal: agentproto.GoalCommand{Objective: req.Objective, TokenBudget: req.TokenBudget, Purpose: "bounded_work"}, IssuedAt: s.now()}
		if previous, e := tx.GetOperation(id); e == nil {
			body, e := tx.GetContent(previous.ScopeID, previous.RequestRef)
			if e != nil {
				return e
			}
			var canonical agentproto.Command
			if e = json.Unmarshal(body.Data, &canonical); e != nil {
				return e
			}
			canonical.IssuedAt = command.IssuedAt
			if !reflect.DeepEqual(canonical, command) {
				return workstore.ErrIdempotencyConflict
			}
			result = Result{Status: "accepted", OperationID: id, Replay: true}
			if previous.State == work.OperationConfirmed {
				result.Status = "completed"
			}
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		raw, e := json.Marshal(command)
		if e != nil {
			return e
		}
		body := work.NewContent(id+":request", c.Worker.ScopeID, "application/json", raw, s.now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		source := work.SourceRef{Kind: "attempt", ID: c.Attempt.ID, Version: fmt.Sprint(c.Attempt.RunRef.Generation), EvidenceRef: body.ID}
		encoded, _ := json.Marshal(source)
		op := work.Operation{ID: id, ScopeID: c.Worker.ScopeID, ContextID: c.Worker.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: "session.goal", TargetID: c.Worker.ID, TargetGeneration: c.Worker.Generation, IdempotencyKey: req.Key, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Steps: []work.OperationStep{{ID: "execution_source", State: work.OperationConfirmed, Receipt: string(encoded)}}, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		canonical, created, e := tx.PutOperation(op, 0)
		if e != nil {
			return e
		}
		if !created || canonical.ID != id {
			return workstore.ErrIdempotencyConflict
		}
		send = true
		instance = c.Worker.InstanceID
		result = Result{Status: "accepted", OperationID: id}
		return nil
	})
	if err != nil || !send {
		return
	}
	if err = s.nativeDispatcher.SendCommand(ctx, instance, command); err != nil {
		saveErr := s.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			op, e := tx.GetOperation(command.CommandID)
			if e != nil {
				return e
			}
			before := op.Revision
			op.Revision++
			op.State = work.OperationUnknown
			op.LastError = err.Error()
			_, _, e = tx.PutOperation(op, before)
			return e
		})
		if saveErr != nil {
			err = fmt.Errorf("%v; preserving send uncertainty: %w", err, saveErr)
		}
	}
	return
}
