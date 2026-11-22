package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var errTeamReviewLocation = errors.New("review needs a selected location")

func parseTeamReviewTarget(text string) (agentproto.ReviewTarget, error) {
	args := strings.Fields(text)
	if len(args) > 0 && strings.TrimPrefix(args[0], "/") == "review" {
		args = args[1:]
	}
	target := agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges}
	if len(args) == 0 {
		return target, nil
	}
	switch args[0] {
	case "uncommitted", "changes":
		if len(args) != 1 {
			return target, fmt.Errorf("用法：/review uncommitted")
		}
	case "commit":
		if len(args) != 2 {
			return target, fmt.Errorf("用法：/review commit <提交>")
		}
		target.Kind = agentproto.ReviewTargetKindCommit
		target.CommitSHA = args[1]
	case "branch":
		if len(args) != 2 {
			return target, fmt.Errorf("用法：/review branch <基准分支>")
		}
		target.Kind = agentproto.ReviewTargetKindBaseBranch
		target.Branch = args[1]
	default:
		target.Kind = agentproto.ReviewTargetKindCustom
		target.Instructions = strings.Join(args, " ")
	}
	return target, nil
}

// Reviews get their own context and fresh read-only native carrier. The source
// context and its live worker are never repurposed as a review session.
func (a *App) queueTeamReview(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	target, err := parseTeamReviewTarget(action.Text)
	if err != nil {
		return "", err
	}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, proof, e := tx.ValidatePrivateAction(source.Authority)
		if e != nil {
			return e
		}
		front, _, e := worksettings.FrozenContext(tx, source.Authority)
		if e != nil {
			return e
		}
		if front.Purpose == work.PurposeControl || front.Purpose == work.PurposeReview {
			return workstore.ErrScopeDenied
		}
		id := teamID("review-context", source.Key)
		if old, e := tx.GetContext(id); e == nil {
			if old.Review == nil || old.Review.Target != target {
				return workstore.ErrIdempotencyConflict
			}
			return nil
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		resourceScope := front.ScopeID
		if front.ProjectViewScopeID != "" {
			resourceScope = front.ProjectViewScopeID
		}
		preference, e := tx.GetExecutionPreference(resourceScope)
		locationID := ""
		if e == nil {
			locationID = preference.DefaultLocationID
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		if locationID == "" {
			workers, e := tx.ListWorkers(front.ScopeID)
			if e != nil {
				return e
			}
			for _, worker := range workers {
				if worker.ContextID == front.ID && worker.State != work.WorkerRetired {
					locationID = worker.LocationID
					break
				}
			}
		}
		if locationID == "" {
			return errTeamReviewLocation
		}
		location, e := tx.GetLocation(locationID)
		if e != nil {
			return e
		}
		now := time.Now().UTC()
		wc := work.WorkContext{ID: id, ScopeID: scope.ID, RoleID: scope.RoleID, Purpose: work.PurposeReview, Workstream: id, Review: &work.ReviewSpec{SourceContextID: front.ID, ResourceScopeID: resourceScope, LocationID: location.ID, LocationGeneration: location.Generation, Target: target, Source: proof}, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if e = tx.PutContext(wc, 0); e != nil {
			return e
		}
		text := "请独立进行只读代码审查，返回评审正文、问题位置与影响。不要修改文件、执行写操作、声明业务验收或完成任务。审查范围：" + string(target.Kind)
		if target.Branch != "" {
			text += " " + target.Branch
		}
		if target.CommitSHA != "" {
			text += " " + target.CommitSHA
		}
		if target.Instructions != "" {
			text += "\n" + target.Instructions
		}
		body := work.NewContent(id+":request", scope.ID, "text/plain", []byte(text), now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		message := work.Message{ID: id + ":message", ScopeID: scope.ID, ConversationID: id, Source: work.Actor{Kind: work.ActorSystem, ID: "human-review-request"}, DerivedFrom: &proof, BodyRef: body.ID, Revision: 1, CreatedAt: now}
		input := work.Inbox{ID: id + ":input", ScopeID: scope.ID, RecipientRoleID: scope.RoleID, RecipientContextID: id, EventID: id, MessageID: message.ID, State: work.InboxPending, Revision: 1, AvailableAt: now}
		_, _, e = tx.AcceptMessage(message, []work.Inbox{input})
		return e
	})
	if errors.Is(err, errTeamReviewLocation) {
		if source.Authority.InteractionID != "" {
			return a.queuePrivatePanel(ctx, source, "review", "请先设置默认审查目录，再发起审查。")
		}
		if err = a.rememberTeamPendingReview(ctx, source, action); err != nil {
			return "", err
		}
		return a.queueTeamResourcePicker(ctx, source)
	}
	if err != nil {
		return "", err
	}
	return a.queuePrivatePanel(ctx, source, "status", "已安排独立只读审查。完成后，评审正文会回到当前工作。")
}

type teamPendingReview struct {
	Source teamHumanSource
	Action control.Action
}

func (a *App) rememberTeamPendingReview(ctx context.Context, source teamHumanSource, action control.Action) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		wc, _, err := worksettings.FrozenContext(tx, source.Authority)
		if err != nil {
			return err
		}
		id := teamID("pending-review", source.Key)
		if _, err = tx.GetOperation(id); err == nil {
			return nil
		} else if err != workstore.ErrNotFound {
			return err
		}
		raw, err := json.Marshal(teamPendingReview{Source: source, Action: action})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		body := work.NewContent(id+":request", source.ScopeID, "application/json", raw, now)
		if err = tx.PutContent(body); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{ID: id, ScopeID: source.ScopeID, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, Kind: "private.review.await_location", TargetID: wc.ID, TargetGeneration: wc.Revision, IdempotencyKey: source.Key, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: now, UpdatedAt: now}, 0)
		return err
	})
}
func (a *App) resumeTeamPendingReview(ctx context.Context, source teamHumanSource, contextID string) (string, bool, error) {
	var pending []work.Operation
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		list, err := tx.ListOperations(source.ScopeID)
		if err != nil {
			return err
		}
		for _, op := range list {
			if op.Kind == "private.review.await_location" && op.State == work.OperationPrepared && op.ContextID == contextID && op.Actor.Kind == work.ActorPrincipal && op.Actor.ID == source.PrincipalID {
				pending = append(pending, op)
			}
		}
		return nil
	}); err != nil {
		return "", false, err
	}
	result := ""
	for _, op := range pending {
		var req teamPendingReview
		if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			body, err := tx.GetContent(op.ScopeID, op.RequestRef)
			if err != nil {
				return err
			}
			return json.Unmarshal(body.Data, &req)
		}); err != nil {
			return "", false, err
		}
		receipt, err := a.queueTeamReview(ctx, req.Source, req.Action)
		if err != nil {
			return "", false, err
		}
		if err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			if _, err := tx.GetContext(teamID("review-context", req.Source.Key)); err != nil {
				return err
			}
			current, err := tx.GetOperation(op.ID)
			if err != nil {
				return err
			}
			before := current.Revision
			current.Revision++
			current.State = work.OperationSending
			if _, _, err = tx.PutOperation(current, before); err != nil {
				return err
			}
			before = current.Revision
			current.Revision++
			current.State = work.OperationConfirmed
			current.ExternalReceipt = receipt
			_, _, err = tx.PutOperation(current, before)
			return err
		}); err != nil {
			return "", false, err
		}
		result = receipt
	}
	return result, len(pending) > 0, nil
}
