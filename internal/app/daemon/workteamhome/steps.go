package workteamhome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type receipt struct{ FolderID, DocumentID, URL string }

// Sending is persisted before I/O. Creates/appends are never repeated after an
// uncertain outcome; a human can connect a read-verified existing original.
func (s *Service) step(ctx context.Context, team work.Team, name string, write bool, call func() (receipt, error)) (result receipt, err error) {
	h := team.Materials
	id := key(team.ID, fmt.Sprint(h.Generation), name)
	var op work.Operation
	skip := false
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetTeam(team.ID)
		if e != nil {
			return e
		}
		if current.Materials == nil || current.Materials.Generation != h.Generation {
			return workstore.ErrConflict
		}
		binding, e := tx.GetRoleBotBinding(h.Identity.RoleID)
		if e != nil {
			return e
		}
		member, e := tx.GetTeamMember(team.ID, h.Identity.RoleID)
		if e != nil {
			return e
		}
		if !member.Active {
			return fmt.Errorf("团队资料机器人已停用，请重新选择现有首页")
		}
		if binding != h.Identity {
			return workstore.ErrConflict
		}
		op, e = tx.GetOperation(id)
		if e == nil {
			if op.ExternalReceipt != "" {
				if e = json.Unmarshal([]byte(op.ExternalReceipt), &result); e != nil {
					return e
				}
			}
			if op.State == work.OperationConfirmed {
				skip = true
				return nil
			}
			if write && (op.State == work.OperationSending || op.State == work.OperationUnknown) {
				return fmt.Errorf("团队资料建立结果待核查；请选择实际已有首页继续，原操作不会重放")
			}
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		} else {
			raw, _ := json.Marshal(struct {
				TeamID, Step string
				Home         *work.TeamMaterials
			}{team.ID, name, h})
			body := work.NewContent(id+":intent", h.ScopeID, "application/json", raw, now())
			if e = tx.PutContent(body); e != nil {
				return e
			}
			op = work.Operation{ID: id, ScopeID: h.ScopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "team-materials"}, Kind: "team.materials." + name, TargetID: team.ID, TargetGeneration: h.Generation, IdempotencyKey: id, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: now(), UpdatedAt: now()}
			if _, _, e = tx.PutOperation(op, 0); e != nil {
				return e
			}
		}
		previous := op.Revision
		op.Revision++
		op.State = work.OperationSending
		op.UpdatedAt = now()
		_, _, e = tx.PutOperation(op, previous)
		return e
	})
	if err != nil || skip {
		return result, err
	}
	result, err = call()
	persistErr := s.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
		fresh, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if fresh.Revision != op.Revision {
			return workstore.ErrConflict
		}
		previous := fresh.Revision
		fresh.Revision++
		fresh.UpdatedAt = now()
		raw, _ := json.Marshal(result)
		fresh.ExternalReceipt = string(raw)
		if err != nil {
			fresh.State = work.OperationUnknown
			fresh.LastError = err.Error()
			var blocked *feishu.PermissionBlockedError
			var api *feishu.APIError
			rejected := errors.As(err, &blocked) || errors.As(err, &api) && (api.Code == 99991672 || api.Code == 99991400 || api.StatusCode == 401 || api.StatusCode == 403 || api.StatusCode == 429)
			if !write || rejected {
				fresh.State = work.OperationRetryable
			}
		} else {
			fresh.State = work.OperationConfirmed
			fresh.LastError = ""
		}
		_, _, e = tx.PutOperation(fresh, previous)
		return e
	})
	return result, errors.Join(err, persistErr)
}
