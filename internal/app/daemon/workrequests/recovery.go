package workrequests

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// RecoverUnsentResponses reuses only a previously saved human decision. It
// does not ask the user again, invent an answer, or replay an uncertain write.
func (s *Service) RecoverUnsentResponses(ctx context.Context, limit int) error {
	s.recoveryMu.Lock()
	var ids []string
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		ids, e = tx.ListRecoverableNativeResponseIDs(s.recoveryAfter, limit, time.Now().UTC())
		return e
	})
	if err == nil {
		if len(ids) == limit {
			s.recoveryAfter = ids[len(ids)-1]
		} else {
			s.recoveryAfter = ""
		}
	}
	s.recoveryMu.Unlock()
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range ids {
		terminal := false
		e := s.store.Within(ctx, func(tx *workstore.Tx) error {
			var err error
			terminal, err = tx.CloseNativeRequestForTerminalTurn(id, time.Now().UTC())
			return err
		})
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if terminal {
			continue
		}
		e = s.store.Within(ctx, func(tx *workstore.Tx) error {
			request, err := tx.GetNativeRequest(id)
			if err != nil {
				return err
			}
			op, err := tx.GetOperation(request.ResponseOperationID)
			if err != nil {
				return err
			}
			if op.State == work.OperationUnknown && op.TargetID == request.ID {
				binding, err := tx.GetRoleBotBinding(request.Binding.RoleID)
				if err != nil {
					return err
				}
				if binding.IdentityGeneration != request.Binding.BotGeneration {
					return workstore.ErrScopeDenied
				}
				_, err = tx.RepairUnregisteredNativeResponseTarget(op.ID, request.ID, time.Now().UTC())
				return err
			}
			return nil
		})
		if e != nil {
			failures = append(failures, e)
			continue
		}
		closed, e := s.closeRetiredControlRequest(ctx, id)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if closed {
			continue
		}
		var decision Decision
		ended := false
		eligible := false
		err = s.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			ended, e = tx.CloseNativeRequestForTerminalTurn(id, time.Now().UTC())
			if e != nil || ended {
				return e
			}
			request, e := tx.GetNativeRequest(id)
			if e != nil {
				return e
			}
			connected, e := tx.CarrierConnected(request.CarrierIncarnation)
			if e != nil {
				return e
			}
			if !connected {
				return nil
			}
			_, e = tx.GetRuntimeGate(request.Binding.WorkerSessionID)
			if e == nil {
				return nil
			}
			if !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			eligible = true
			op, e := tx.GetOperation(request.ResponseOperationID)
			if e != nil {
				return e
			}
			body, e := tx.GetContent(request.ScopeID, op.RequestRef)
			if e != nil {
				return e
			}
			var command agentproto.Command
			if e = json.Unmarshal(body.Data, &command); e != nil {
				return e
			}
			saved, e := tx.GetContent(request.ScopeID, command.Origin.MessageID+":decision")
			if e != nil {
				return e
			}
			return json.Unmarshal(saved.Data, &decision)
		})
		if err == nil && !ended && eligible {
			_, err = s.Respond(ctx, decision)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
