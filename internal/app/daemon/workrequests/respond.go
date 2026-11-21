package workrequests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Driver interface {
	SendCommand(context.Context, string, agentproto.Command) error
}

// Decision is produced by the authenticated UI and the native form translator,
// never by a role's ordinary CLI. EvidenceRef names the saved human action.
type Decision struct {
	RequestID          string
	ExpectedRevision   int64
	PrincipalID        string
	EvidenceRef        string
	FollowupHumanInput string
	Response           agentproto.Request
}

type Authorizer func(context.Context, workstore.NativeRequest, Decision) error
type Service struct {
	store     *workstore.Store
	driver    Driver
	authorize Authorizer
}

func New(store *workstore.Store, driver Driver, authorize Authorizer) (*Service, error) {
	if store == nil || driver == nil || authorize == nil {
		return nil, fmt.Errorf("native request store, driver and authenticated human authorization are required")
	}
	return &Service{store: store, driver: driver, authorize: authorize}, nil
}

// Respond persists the complete native response before transport, and never
// retries an uncertain write. A native resolved event is the completion proof.
func (s *Service) Respond(ctx context.Context, decision Decision) (string, error) {
	if decision.PrincipalID == "" || decision.EvidenceRef == "" || decision.Response.Response == nil {
		return "", fmt.Errorf("human decision and translated response are required")
	}
	var request workstore.NativeRequest
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		request, e = tx.GetNativeRequest(decision.RequestID)
		return e
	})
	if err != nil {
		return "", err
	}
	if err = s.authorize(ctx, request, decision); err != nil {
		return "", err
	}
	commandID := requestID(request.ID, "response")
	var command agentproto.Command
	var instance string
	send := false
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		fresh, e := tx.GetNativeRequest(request.ID)
		if e != nil {
			return e
		}
		if _, e = tx.GetPrincipal(decision.PrincipalID); e != nil {
			return e
		}
		if _, e = tx.GetContent(fresh.ScopeID, decision.EvidenceRef); e != nil {
			return e
		}
		response := decision.Response
		if response.RequestID != "" && response.RequestID != fresh.NativeRequestID {
			return fmt.Errorf("response addresses another native request")
		}
		response.RequestID = fresh.NativeRequestID
		command = agentproto.Command{CommandID: commandID, Kind: agentproto.CommandRequestRespond, Execution: &fresh.Binding, Origin: agentproto.Origin{UserID: decision.PrincipalID, MessageID: decision.EvidenceRef}, Target: agentproto.Target{ThreadID: fresh.NativeSessionID, TurnID: fresh.NativeTurnID}, Request: response}
		body, e := json.Marshal(command)
		if e != nil {
			return e
		}
		content := work.NewContent(commandID+":body", fresh.ScopeID, "application/json", body, time.Now().UTC())
		if op, e := tx.GetOperation(commandID); e == nil {
			if op.RequestHash != content.SHA256 || op.Actor.ID != decision.PrincipalID {
				return workstore.ErrIdempotencyConflict
			}
			if op.State != work.OperationPrepared {
				return nil
			}
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		if fresh.Revision != decision.ExpectedRevision || fresh.State != "open" {
			return fmt.Errorf("request changed or no longer accepts a response")
		}
		carrier, e := tx.GetCarrier(fresh.CarrierIncarnation)
		if e != nil {
			return e
		}
		worker, e := tx.GetWorker(fresh.Binding.WorkerSessionID)
		if e != nil {
			return e
		}
		if !carrier.Matches(worker) || worker.Controller != work.ControllerAutomatic {
			return fmt.Errorf("request belongs to an obsolete or human-controlled carrier")
		}
		bot, e := tx.GetRoleBotBinding(worker.RoleID)
		if e != nil {
			return e
		}
		if bot.IdentityGeneration != fresh.Binding.BotGeneration {
			return fmt.Errorf("request bot identity changed")
		}
		attempt, e := tx.ValidateNativeBinding(fresh.Binding)
		if e != nil {
			return e
		}
		if attempt.State != work.AttemptRunning {
			return fmt.Errorf("request execution is not running")
		}
		gates, e := tx.ListPauseGates(fresh.ScopeID)
		if e != nil {
			return e
		}
		wc, e := tx.GetContext(attempt.ContextID)
		if e != nil {
			return e
		}
		allowed, e := work.GatesAllowRunForPurpose(fresh.ScopeID, attempt.TaskID, attempt.ContextID, wc.Purpose, gates)
		if e != nil {
			return e
		}
		if !allowed {
			return fmt.Errorf("execution is paused; response has not been sent")
		}
		if e = tx.PutContent(content); e != nil {
			return e
		}
		op := work.Operation{ID: commandID, ScopeID: fresh.ScopeID, ContextID: fresh.Binding.ContextID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: decision.PrincipalID}, Kind: "native.request_response", TargetID: fresh.ID, TargetGeneration: fresh.Binding.SessionGeneration, IdempotencyKey: commandID, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		if _, _, e = tx.PutOperation(op, 0); e != nil {
			return e
		}
		previous := fresh.Revision
		fresh.Revision++
		fresh.State = "response_pending"
		fresh.ResponseOperationID = commandID
		fresh.UpdatedAt = time.Now().UTC()
		if e = tx.PutNativeRequest(fresh, previous); e != nil {
			return e
		}
		if decision.FollowupHumanInput != "" {
			interaction, e := tx.GetHumanInteraction(decision.EvidenceRef)
			if e != nil {
				return e
			}
			if interaction.PrincipalID != decision.PrincipalID {
				return workstore.ErrScopeDenied
			}
			if e = saveFollowup(tx, fresh, interaction, decision.FollowupHumanInput); e != nil {
				return e
			}
		}
		instance = carrier.OriginInstanceID
		send = true
		return nil
	})
	if err != nil || !send {
		return commandID, err
	}
	if err = s.driver.SendCommand(ctx, instance, command); err != nil {
		persistErr := s.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			op, e := tx.GetOperation(commandID)
			if e != nil {
				return e
			}
			if op.State == work.OperationConfirmed {
				return nil
			}
			previous := op.Revision
			op.Revision++
			op.State = work.OperationUnknown
			op.UpdatedAt = time.Now().UTC()
			_, _, e = tx.PutOperation(op, previous)
			return e
		})
		return commandID, errors.Join(err, persistErr)
	}
	return commandID, nil
}
