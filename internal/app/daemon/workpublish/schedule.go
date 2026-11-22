package workpublish

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const groupSnapshotInterval = 30 * time.Second
const groupBackgroundInterval = 10 * time.Second

type scheduledPublication struct {
	item          work.Outbox
	op            work.Operation
	projection    workservice.MessageProjection
	group, stream string
	background    bool
}

func scheduleIdentity(tx *workstore.Tx, scope work.WorkScope, op work.Operation, projection workservice.MessageProjection) (group, stream string, background bool, err error) {
	if scope.Kind != work.ScopeProject && scope.Kind != work.ScopeBootstrap {
		return
	}
	group = fmt.Sprintf("group:%q:%q", projection.Tenant, projection.ChatID)
	if projection.UIViewID != "" {
		var view workstore.UIView
		view, err = tx.GetUIView(projection.UIViewID)
		if err != nil {
			return
		}
		if view.Kind == "project.status" && strings.HasPrefix(view.SurfaceKey, "project.status:") {
			stream, background = group+":surface:"+view.SurfaceKey, true
			priority, e := tx.GetContent(view.ScopeID, view.ID+":snapshot-priority")
			if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				err = e
				return
			}
			if e == nil && string(priority.Data) == "urgent" {
				background = false
			}
		}
		return
	}
	root := projection.RootMessageID
	if projection.TaskID != "" {
		root = ""
	}
	stream = fmt.Sprintf("%s:role:%q:task:%q:root:%q", group, projection.RoleID, projection.TaskID, root)
	background = projection.Purpose == "progress"
	return
}

// Select after merging, before applying the batch limit: a noisy project must
// not starve interactive decisions or another project's results. Unknown writes
// are deliberately excluded; replacing an uncertain external effect is unsafe.
func (p *Publisher) selectPublications(tx *workstore.Tx, limit int) ([]string, error) {
	scopes, err := tx.ListScopes()
	if err != nil {
		return nil, err
	}
	var candidates []scheduledPublication
	for _, scope := range scopes {
		items, e := tx.ListPendingPublicationOutbox(scope.ID)
		if e != nil {
			return nil, e
		}
		for _, item := range items {
			if item.State != work.OutboxPending && item.State != work.OutboxRetryable {
				continue
			}
			op, e := tx.GetOperation(item.OperationID)
			if e != nil {
				return nil, e
			}
			if op.Kind != "feishu.publish" {
				continue
			}
			content, e := tx.GetContent(scope.ID, item.PayloadRef)
			if e != nil {
				return nil, e
			}
			var projection workservice.MessageProjection
			if e = json.Unmarshal(content.Data, &projection); e != nil {
				return nil, e
			}
			if e = workservice.ValidatePublication(tx, op, projection); e != nil {
				if errors.Is(e, workservice.ErrPublicationDenied) {
					if e = p.discardSnapshot(tx, scheduledPublication{item: item, op: op}, "publication authority no longer valid"); e != nil {
						return nil, e
					}
					continue
				}
				return nil, e
			}
			group, stream, background, e := scheduleIdentity(tx, scope, op, projection)
			if e != nil {
				return nil, e
			}
			candidates = append(candidates, scheduledPublication{item, op, projection, group, stream, background})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].op.CreatedAt.Equal(candidates[j].op.CreatedAt) {
			return candidates[i].op.ID < candidates[j].op.ID
		}
		return candidates[i].op.CreatedAt.Before(candidates[j].op.CreatedAt)
	})
	latest := map[string]int{}
	for i, candidate := range candidates {
		if candidate.stream != "" && (candidate.background || candidate.projection.Purpose == "final" || candidate.projection.UIViewID != "") {
			latest[candidate.stream] = i
		}
	}
	var ready []scheduledPublication
	for i, candidate := range candidates {
		if candidate.background && latest[candidate.stream] != i {
			if err = p.discardSnapshot(tx, candidate, "superseded by a newer progress snapshot or final result"); err != nil {
				return nil, err
			}
			continue
		}
		if candidate.item.NextAttemptAt != nil && candidate.item.NextAttemptAt.After(p.now()) {
			continue
		}
		if candidate.background {
			blocked := false
			for _, key := range []string{candidate.group, candidate.stream} {
				until, e := tx.PublicationBudgetUntil(key)
				if e != nil {
					return nil, e
				}
				blocked = blocked || until.After(p.now())
			}
			if blocked {
				continue
			}
		}
		ready = append(ready, candidate)
	}
	sort.SliceStable(ready, func(i, j int) bool { return !ready[i].background && ready[j].background })
	ids := make([]string, 0, len(ready))
	for _, candidate := range ready {
		ids = append(ids, candidate.item.ID)
	}
	return ids, nil
}

func (p *Publisher) discardSnapshot(tx *workstore.Tx, candidate scheduledPublication, reason string) error {
	op, item := candidate.op, candidate.item
	oldOp, oldItem := op.Revision, item.Revision
	op.State, op.LastError, op.Revision, op.UpdatedAt = work.OperationTerminal, reason, op.Revision+1, p.now()
	item.State, item.Revision = work.OutboxTerminal, item.Revision+1
	if _, _, err := tx.PutOperation(op, oldOp); err != nil {
		return err
	}
	return tx.PutOutbox(item, oldItem)
}

// Reserve inside the same claim transaction as the external operation. A
// restart, a second publisher, or another Bot cannot bypass this shared window.
func (p *Publisher) reserveBackground(tx *workstore.Tx, scope work.WorkScope, op work.Operation, projection workservice.MessageProjection) error {
	group, stream, background, err := scheduleIdentity(tx, scope, op, projection)
	if err != nil || !background {
		return err
	}
	for _, key := range []string{group, stream} {
		until, e := tx.PublicationBudgetUntil(key)
		if e != nil {
			return e
		}
		if until.After(p.now()) {
			return workstore.ErrConflict
		}
	}
	if err = tx.SetPublicationBudget(group, p.now().Add(groupBackgroundInterval)); err != nil {
		return err
	}
	return tx.SetPublicationBudget(stream, p.now().Add(groupSnapshotInterval))
}
