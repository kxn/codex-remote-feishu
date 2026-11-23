// Package workreaction projects durable private input state onto the original
// human messages. It has no execution authority and never reads legacy console state.
package workreaction

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

type Sender interface {
	CreateMessageReaction(context.Context, string, string) (string, error)
	DeleteMessageReaction(context.Context, string, string) error
	ListMessageReactions(context.Context, string, string) ([]feishu.ProjectReaction, error)
}
type Service struct {
	Store    *workstore.Store
	Resolver workpublish.Resolver
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) RunOnce(ctx context.Context, limit int) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("reaction batch must be 1..100")
	}
	var pending []workstore.PrivateReaction
	if err := s.Store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.BackfillPrivateInputReactions(limit); e != nil {
			return e
		}
		var e error
		pending, e = tx.ListPendingPrivateReactions(s.now(), limit)
		return e
	}); err != nil {
		return err
	}
	var failures []error
	for _, v := range pending {
		if err := s.advance(ctx, v.ID); err != nil && !errors.Is(err, workstore.ErrConflict) {
			failures = append(failures, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(failures...)
}
func (s *Service) save(ctx context.Context, v workstore.PrivateReaction) error {
	return s.Store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
		// An input may have changed during HTTP. Preserve its newer wakeup but merge
		// this exact receipt; otherwise the next iteration reconciles InFlight by GET.
		current, err := tx.GetPrivateReaction(v.ID)
		if err != nil {
			return err
		}
		if current.Revision != v.Revision {
			return workstore.ErrConflict
		}
		previous := v.Revision
		v.Revision++
		v.UpdatedAt = s.now()
		return tx.PutPrivateReaction(v, previous)
	})
}
func (s *Service) advance(ctx context.Context, id string) error {
	for step := 0; step < 8; step++ {
		var v workstore.PrivateReaction
		var desired []string
		var terminal, obsolete bool
		err := s.Store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			v, e = tx.GetPrivateReaction(id)
			if e != nil {
				return e
			}
			binding, e := tx.GetRoleBotBinding(v.Binding.RoleID)
			if e != nil {
				return e
			}
			obsolete = binding != v.Binding
			if obsolete {
				return nil
			}
			desired, terminal, e = tx.PrivateInputReactionState(v)
			return e
		})
		if errors.Is(err, workstore.ErrPrivateReactionSourceGone) {
			v.Finished = true
			v.InFlightAction, v.InFlightEmoji = "", ""
			v.ReactionIDs = map[string]string{}
			return s.save(ctx, v)
		}
		if err != nil {
			return err
		}
		if v.Finished {
			return nil
		}
		if obsolete {
			v.Finished = true
			return s.save(ctx, v)
		}
		wanted := map[string]bool{}
		for _, emoji := range desired {
			wanted[emoji] = true
		}
		if v.ReactionIDs == nil {
			v.ReactionIDs = map[string]string{}
		}
		action, emoji := v.InFlightAction, v.InFlightEmoji
		if action == "" {
			for _, candidate := range []string{"OneSecond", "THINKING", "THUMBSUP", "ThumbsDown", "APPLAUSE"} {
				if v.ReactionIDs[candidate] != "" && !wanted[candidate] {
					action, emoji = "delete", candidate
					break
				}
			}
			if action == "" {
				for _, candidate := range desired {
					if v.ReactionIDs[candidate] == "" {
						action, emoji = "create", candidate
						break
					}
				}
			}
			if action == "" {
				v.Finished = terminal
				v.NextAt = s.now().Add(time.Second)
				v.LastError = ""
				return s.save(ctx, v)
			}
		}
		resolved, e := s.Resolver.Resolve(ctx, v.Binding)
		if e != nil {
			return s.deferFailure(ctx, v, e)
		}
		sender, ok := resolved.(Sender)
		if !ok {
			return s.deferFailure(ctx, v, fmt.Errorf("selected bot has no reaction adapter"))
		}
		if v.InFlightAction != "" {
			// A sending marker may outlive its process or HTTP response. GET establishes
			// what this exact application actually owns before another write is allowed.
			reactions, e := sender.ListMessageReactions(ctx, v.Message.MessageID, emoji)
			if e != nil {
				return s.deferFailure(ctx, v, e)
			}
			found := ""
			for _, reaction := range reactions {
				if reaction.Emoji == emoji && reaction.OperatorType == "app" && reaction.OperatorID == v.Binding.AppID {
					found = reaction.ID
					break
				}
			}
			if found == "" {
				delete(v.ReactionIDs, emoji)
			} else {
				v.ReactionIDs[emoji] = found
			}
			v.InFlightAction, v.InFlightEmoji = "", ""
			v.LastError = ""
			v.NextAt = time.Time{}
			if e = s.save(ctx, v); e != nil {
				return e
			}
			continue
		}
		v.InFlightAction, v.InFlightEmoji = action, emoji
		v.NextAt = time.Time{}
		if e = s.save(ctx, v); e != nil {
			return e
		}
		v.Revision++
		if action == "create" {
			receipt, e := sender.CreateMessageReaction(ctx, v.Message.MessageID, emoji)
			if e != nil {
				return s.deferFailure(ctx, v, e)
			}
			v.ReactionIDs[emoji] = receipt
		} else {
			if e = sender.DeleteMessageReaction(ctx, v.Message.MessageID, v.ReactionIDs[emoji]); e != nil {
				return s.deferFailure(ctx, v, e)
			}
			delete(v.ReactionIDs, emoji)
		}
		v.InFlightAction, v.InFlightEmoji = "", ""
		v.LastError = ""
		v.NextAt = time.Time{}
		if e = s.save(ctx, v); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) deferFailure(ctx context.Context, v workstore.PrivateReaction, cause error) error {
	v.LastError = cause.Error()
	v.NextAt = s.now().Add(10 * time.Second)
	if err := s.save(ctx, v); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}
