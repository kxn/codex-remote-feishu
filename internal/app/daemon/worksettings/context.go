package worksettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (s *Service) UpdateContext(ctx context.Context, principal string, source workstore.PrivateActionSource, request ContextUpdateRequest) (work.ContextPreferences, error) {
	var result work.ContextPreferences
	if request.Key == "" || len(request.Key) > 256 {
		return result, fmt.Errorf("context settings require an idempotency key")
	}
	if request.AccessMode == nil && request.PlanMode == nil && request.Verbose == nil && len(request.Reset) == 0 {
		return result, fmt.Errorf("choose an explicit context preference")
	}
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, _, err := tx.ValidatePrivateAction(source)
		if err != nil {
			return err
		}
		if scope.PrincipalID != principal {
			return workstore.ErrScopeDenied
		}
		context, err := tx.GetContext(request.ContextID)
		if err != nil {
			return err
		}
		if context.ScopeID != scope.ID || context.RoleID != scope.RoleID {
			return workstore.ErrScopeDenied
		}
		raw, err := json.Marshal(struct {
			Request ContextUpdateRequest
			Source  workstore.PrivateActionSource
		}{request, source})
		if err != nil {
			return err
		}
		body := work.NewContent(stableID("context_setting_request", scope.ID, principal, request.Key), scope.ID, "application/json", raw, s.now())
		opID := stableID("context_settings", scope.ID, principal, request.Key)
		if op, err := tx.GetOperation(opID); err == nil {
			if op.RequestHash != body.SHA256 {
				return workstore.ErrIdempotencyConflict
			}
			return json.Unmarshal([]byte(op.ExternalReceipt), &result)
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		_, _, ref, err := validateSource(tx, principal, scope.RoleID, source)
		if err != nil {
			return err
		}
		old, err := tx.GetContextPreferences(context.ID)
		if errors.Is(err, workstore.ErrNotFound) {
			old = work.ContextPreferences{ContextID: context.ID, ScopeID: scope.ID}
		} else if err != nil {
			return err
		}
		if old.Revision != request.ExpectedRevision {
			return workstore.ErrConflict
		}
		next := old
		seen := map[string]bool{}
		for _, reset := range request.Reset {
			if seen[reset] {
				return fmt.Errorf("duplicate preference reset")
			}
			seen[reset] = true
			switch reset {
			case "access":
				if request.AccessMode != nil {
					return fmt.Errorf("cannot set and reset access together")
				}
				next.AccessMode = nil
			case "plan":
				if request.PlanMode != nil {
					return fmt.Errorf("cannot set and reset plan together")
				}
				next.PlanMode = nil
			case "verbose":
				if request.Verbose != nil {
					return fmt.Errorf("cannot set and reset verbose together")
				}
				next.Verbose = nil
			default:
				return fmt.Errorf("unknown context preference reset")
			}
		}
		if request.AccessMode != nil {
			canonical := agentproto.NormalizeAccessMode(*request.AccessMode)
			if canonical == "" {
				return fmt.Errorf("invalid access mode")
			}
			next.AccessMode = &canonical
		}
		if request.PlanMode != nil {
			canonical := strings.ToLower(strings.TrimSpace(*request.PlanMode))
			if canonical != "on" && canonical != "off" {
				return fmt.Errorf("plan mode must be on or off")
			}
			next.PlanMode = &canonical
		}
		if request.Verbose != nil {
			value := strings.ToLower(strings.TrimSpace(*request.Verbose))
			if value != "quiet" && value != "normal" && value != "verbose" && value != "chatty" {
				return fmt.Errorf("verbosity must be quiet, normal, verbose or chatty")
			}
			next.Verbose = &value
		}
		next.Revision++
		next.Source = ref
		next.UpdatedBy = work.Actor{Kind: work.ActorPrincipal, ID: principal}
		next.UpdatedAt = s.now()
		if err = tx.PutContextPreferences(next, request.ExpectedRevision, source); err != nil {
			return err
		}
		if err = tx.PutContent(body); err != nil {
			return err
		}
		op := work.Operation{ID: opID, ScopeID: scope.ID, ContextID: context.ID, Actor: next.UpdatedBy, Kind: "context.settings.update", TargetID: context.ID, TargetGeneration: 1, IdempotencyKey: request.Key, RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		op.State = work.OperationSending
		op.Revision++
		if _, _, err = tx.PutOperation(op, 1); err != nil {
			return err
		}
		receipt, err := json.Marshal(next)
		if err != nil {
			return err
		}
		op.State = work.OperationConfirmed
		op.Revision++
		op.ExternalReceipt = string(receipt)
		if _, _, err = tx.PutOperation(op, 2); err != nil {
			return err
		}
		result = next
		return nil
	})
	return result, err
}
