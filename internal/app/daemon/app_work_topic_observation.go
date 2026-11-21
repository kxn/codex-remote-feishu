package daemon

import (
	"context"
	"encoding/json"
	"log"
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Probe only stalled publications, oldest observation first. Network reads run
// outside the SQLite transaction and this independent recovery lane is bounded.
func (a *App) observeFailedTeamTopics(ctx context.Context) error {
	if a.teamRuntime.publisher == nil {
		return nil
	}
	type candidate struct{ scope, task, last string }
	var candidates []candidate
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, e := tx.ListScopes()
		if e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, scope := range scopes {
			outboxes, e := tx.ListOutbox(scope.ID)
			if e != nil {
				return e
			}
			for _, item := range outboxes {
				if item.State != work.OutboxUnknown && item.State != work.OutboxTerminal && item.State != work.OutboxRetryable {
					continue
				}
				op, e := tx.GetOperation(item.OperationID)
				if e != nil {
					return e
				}
				if op.Kind != "feishu.publish" {
					continue
				}
				reanchored := false
				for _, step := range op.Steps {
					reanchored = reanchored || step.ID == "topic_reanchored"
				}
				if reanchored {
					continue
				}
				body, e := tx.GetContent(scope.ID, item.PayloadRef)
				if e != nil {
					return e
				}
				var projection workservice.MessageProjection
				if e = json.Unmarshal(body.Data, &projection); e != nil {
					return e
				}
				if projection.TaskID == "" {
					continue
				}
				key := scope.ID + ":" + projection.TaskID
				if seen[key] {
					continue
				}
				seen[key] = true
				conversation, e := tx.GetTaskConversation(scope.ID, projection.TaskID)
				if e == workstore.ErrNotFound {
					continue
				}
				if e != nil {
					return e
				}
				unavailable, e := tx.TaskTopicUnavailable(conversation)
				if e != nil {
					return e
				}
				if unavailable {
					continue
				}
				last, e := tx.LastTopicObservation(scope.ID, conversation.RootMessageID)
				if e != nil {
					return e
				}
				candidates = append(candidates, candidate{scope.ID, projection.TaskID, last})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].last != candidates[j].last {
			return candidates[i].last < candidates[j].last
		}
		return candidates[i].scope+candidates[i].task < candidates[j].scope+candidates[j].task
	})
	if len(candidates) > 16 {
		candidates = candidates[:16]
	}
	for _, item := range candidates {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = a.teamRuntime.publisher.ObserveTaskTopic(ctx, item.scope, item.task); err != nil {
			log.Printf("team topic observation %s: %v", item.task, err)
		}
	}
	return nil
}
