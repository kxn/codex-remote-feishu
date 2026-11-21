package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type publicationRecoveryTarget struct {
	OutboxID          string `json:"outbox_id"`
	OperationID       string `json:"operation_id"`
	TaskID            string `json:"task_id,omitempty"`
	OperationRevision int64  `json:"operation_revision"`
	OutboxRevision    int64  `json:"outbox_revision"`
	TopicRevision     int64  `json:"topic_revision"`
}

// runTeamPublicationRecoveryViews never creates a recovery notice for another
// recovery notice. Identical unknown facts produce only one durable view.
func (a *App) runTeamPublicationRecoveryViews(ctx context.Context) error {
	r := a.teamRuntime
	if r == nil {
		return nil
	}
	if err := a.observeFailedTeamTopics(ctx); err != nil {
		return err
	}
	return r.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			outboxes, err := tx.ListOutbox(scope.ID)
			if err != nil {
				return err
			}
			for _, outbox := range outboxes {
				if outbox.State == work.OutboxSending || outbox.State == work.OutboxSent {
					continue
				}
				op, err := tx.GetOperation(outbox.OperationID)
				if err != nil {
					return err
				}
				if op.Kind != "feishu.publish" || op.State == work.OperationSending || op.State == work.OperationConfirmed {
					continue
				}
				detached := false
				for _, step := range op.Steps {
					detached = detached || step.ID == "topic_reanchored"
				}
				if detached {
					continue
				}
				body, err := tx.GetContent(scope.ID, outbox.PayloadRef)
				if err != nil {
					return err
				}
				var projection workservice.MessageProjection
				if err = json.Unmarshal(body.Data, &projection); err != nil {
					return err
				}
				for _, step := range op.Steps {
					if step.ID == "send_projection" {
						frozen, err := tx.GetContent(scope.ID, step.Receipt)
						if err != nil {
							return err
						}
						if err = json.Unmarshal(frozen.Data, &projection); err != nil {
							return err
						}
						break
					}
				}
				if projection.UIViewID != "" {
					source, err := tx.GetUIView(projection.UIViewID)
					if err != nil {
						return err
					}
					if strings.HasPrefix(source.Kind, "publication.recovery") || strings.HasPrefix(source.Kind, "project.recovery") {
						continue
					}
				}
				target := publicationRecoveryTarget{OutboxID: outbox.ID, OperationID: op.ID, TaskID: projection.TaskID, OperationRevision: op.Revision, OutboxRevision: outbox.Revision}
				rootRecalled := false
				if target.TaskID != "" {
					conversation, err := tx.GetTaskConversation(scope.ID, target.TaskID)
					if err == nil {
						target.TopicRevision = conversation.Revision
						rootRecalled, err = tx.TaskTopicUnavailable(conversation)
						if err != nil {
							return err
						}
					} else if err != workstore.ErrNotFound {
						return err
					}
				}
				if op.State != work.OperationUnknown && !rootRecalled {
					continue
				}
				id := workui.ID("publication.recovery", op.ID, strconv.FormatInt(op.Revision, 10), strconv.FormatInt(outbox.Revision, 10), strconv.FormatInt(target.TopicRevision, 10), strconv.FormatBool(rootRecalled))
				if _, err = tx.GetUIView(id); err == nil {
					continue
				} else if err != workstore.ErrNotFound {
					return err
				}
				binding, err := tx.GetRoleBotBinding(projection.RoleID)
				if err != nil {
					return err
				}
				if binding.GatewayRef != projection.GatewayRef || binding.AppID != projection.AppID || binding.IdentityGeneration != projection.BotGeneration {
					continue
				}
				role, err := tx.GetRole(binding.RoleID)
				if err != nil {
					return err
				}
				panel := workui.Panel{Title: role.Name + "的一条回复需要核对", Paragraphs: []string{"发送请求已发出，但没有可靠回执。未把它当作已发送，也没有自动重复发送。原内容和记录仍保留。"}}
				if rootRecalled {
					panel.Title = role.Name + "的工作话题需要恢复"
					panel.Paragraphs = []string{"原来的话题消息已被撤回，后续回复无法投到原处。旧记录已保留，不会擅自改投最近一条消息。"}
				}
				if target.TaskID != "" {
					task, err := tx.GetTask(target.TaskID)
					if err != nil {
						return err
					}
					panel.Paragraphs = append(panel.Paragraphs, "关联工作："+task.Title)
				}
				started := time.Time{}
				for _, step := range op.Steps {
					if step.ID == "first_send" {
						started, _ = time.Parse(time.RFC3339Nano, step.Receipt)
					}
				}
				if projection.ReplaceMessageID != "" {
					panel.Paragraphs = append(panel.Paragraphs, "这是一次卡片更新；不会重放旧更新来覆盖您之后的选择。请重新打开当前页面继续操作。")
				} else if op.State == work.OperationUnknown && !rootRecalled && !started.IsZero() && !time.Now().Before(started) && time.Since(started) <= 55*time.Minute {
					panel.Paragraphs = append(panel.Paragraphs, "仍在平台去重有效期内，可以按原内容、原话题和同一个发送标识核对重发。")
					panel.Buttons = append(panel.Buttons, workui.Button{Label: "核对并重发原回复", Action: "publication.retry"})
				} else {
					panel.Paragraphs = append(panel.Paragraphs, "已超出去重有效期，不能凭相同文字认领另一条消息或直接重发。")
				}
				reservation := ""
				if target.TaskID != "" {
					reservation, _ = tx.GetTaskTopicReservation(scope.ID, target.TaskID)
				}
				if target.TaskID != "" && projection.ReplaceMessageID == "" && (target.TopicRevision == 0 && reservation == op.ID || target.TopicRevision > 0 && rootRecalled) {
					panel.Buttons = append(panel.Buttons, workui.Button{Label: "从这里继续后续进展", Action: "publication.reanchor"})
					panel.Paragraphs = append(panel.Paragraphs, "继续后，本卡会作为后续进展的话题入口；原发送仍保留为待核查，不会补称已发送。")
				}
				card, err := workui.Card(id, panel)
				if err != nil {
					return err
				}
				raw, _ := json.Marshal(target)
				if err = tx.PutContent(work.NewContent(id+":publication-recovery", scope.ID, "application/json", raw, time.Now().UTC())); err != nil {
					return err
				}
				principal := ""
				if scope.Kind == work.ScopePersonal {
					principal = scope.PrincipalID
				}
				view := workstore.UIView{ID: id, ScopeID: scope.ID, ContextID: op.ContextID, SurfaceKey: "publication.recovery:" + outbox.ID, Kind: "publication.recovery", TargetID: outbox.ID, TargetRevision: op.Revision, Binding: binding, Tenant: projection.Tenant, ChatID: projection.ChatID, ChannelGeneration: projection.ChannelGeneration, PrincipalID: principal}
				if _, err = workui.Queue(tx, view, card); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (a *App) publicationRecoveryAuthority(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView) (publicationRecoveryTarget, error) {
	var target publicationRecoveryTarget
	if view.Kind != "publication.recovery" {
		return target, workstore.ErrScopeDenied
	}
	project := false
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, err := tx.GetScope(view.ScopeID)
		project = scope.Kind == work.ScopeProject
		return err
	})
	if err != nil {
		return target, err
	}
	if project {
		if err = a.verifyWorkMembership(ctx, interaction.PrincipalID, view.ScopeID, interaction.Envelope.Tenant, interaction.Envelope.SenderIDs, interaction.Gateway); err != nil {
			return target, err
		}
	}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		if project {
			if err := tx.RequireProjectPrincipalAccess(view.ScopeID, interaction.PrincipalID, time.Now()); err != nil {
				return err
			}
		}
		content, err := tx.GetContent(view.ScopeID, view.ID+":publication-recovery")
		if err != nil {
			return err
		}
		if err = json.Unmarshal(content.Data, &target); err != nil {
			return err
		}
		outbox, err := tx.GetOutbox(target.OutboxID)
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(target.OperationID)
		if err != nil {
			return err
		}
		reanchored := false
		if action, _ := interaction.Envelope.Values["action"].(string); action == "publication.reanchor" {
			if audit, e := tx.GetOperation("topic-reanchor:" + interaction.ID); e == nil && audit.ScopeID == view.ScopeID && audit.TargetID == target.TaskID && audit.ExternalReceipt == interaction.Envelope.MessageID {
				reanchored = true
			}
		}
		if outbox.ScopeID != view.ScopeID || op.ScopeID != view.ScopeID || outbox.OperationID != op.ID || !reanchored && (outbox.Revision != target.OutboxRevision || op.Revision != target.OperationRevision) || view.TargetID != outbox.ID || !reanchored && view.TargetRevision != op.Revision {
			return workstore.ErrConflict
		}
		return nil
	})
	return target, err
}
func (a *App) handleTeamPublicationRecovery(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	target, err := a.publicationRecoveryAuthority(ctx, interaction, view)
	if err != nil {
		return err
	}
	if action == "publication.reanchor" {
		if err := a.teamRuntime.publisher.Reanchor(ctx, workpublish.ReanchorRequest{InteractionID: interaction.ID, ViewID: view.ID, OutboxID: target.OutboxID, OperationID: target.OperationID, TaskID: target.TaskID, OperationRevision: target.OperationRevision, OutboxRevision: target.OutboxRevision, TopicRevision: target.TopicRevision}); err != nil {
			return err
		}
		return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			noticeID := workui.ID("publication.reanchor.result", interaction.ID)
			card, err := workui.Card(noticeID, workui.Panel{Title: "后续进展从这里继续", Paragraphs: []string{"这条消息已成为后续进展的话题入口。之前失败或未确认发送的记录仍保留，不会补称已发送。"}})
			if err != nil {
				return err
			}
			notice := view
			notice.ID = noticeID
			notice.ExternalMessageID = ""
			notice.ReplacesViewID = ""
			notice.BodyRef = ""
			notice.OperationID = ""
			notice.Invalidated = false
			notice.Revision = 0
			notice.Kind = "publication.recovery.result"
			notice.SurfaceKey = "publication.recovery.result:" + target.OutboxID
			notice.TargetID = "topic-reanchor:" + interaction.ID
			notice.TargetRevision = 1
			notice.ReplyToMessageID = interaction.Envelope.MessageID
			if _, err = workui.Queue(tx, notice, card); err != nil {
				return err
			}
			return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, "topic-reanchor:"+interaction.ID)
		})
	}
	if action != "publication.retry" {
		return fmt.Errorf("this publication recovery action requires its dedicated trusted handler")
	}
	r := a.teamRuntime
	id := workui.ID("publication-human-retry", interaction.ID)
	proceed := false
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		if old, e := tx.GetOperation(id); e == nil {
			if old.State == work.OperationConfirmed {
				return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, id)
			}
			return fmt.Errorf("这次恢复的结果尚待核对，未再次发送")
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		proof, _ := json.Marshal(struct {
			Target      publicationRecoveryTarget
			Interaction workstore.HumanInteraction
		}{target, interaction})
		content := work.NewContent(id+":proof", view.ScopeID, "application/json", proof, time.Now().UTC())
		if err := tx.PutContent(content); err != nil {
			return err
		}
		_, _, err := tx.PutOperation(work.Operation{ID: id, ScopeID: view.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}, Kind: "publication.recovery", TargetID: target.OperationID, TargetGeneration: target.OperationRevision, IdempotencyKey: id, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, 0)
		proceed = err == nil
		return err
	})
	if err != nil || !proceed {
		return err
	}
	recoveryErr := r.publisher.RetryUnknown(ctx, target.OutboxID)
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = r.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		expected := op.Revision
		op.Revision++
		op.UpdatedAt = time.Now().UTC()
		outbox, err := tx.GetOutbox(target.OutboxID)
		if err != nil {
			return err
		}
		if recoveryErr == nil && outbox.State == work.OutboxSent {
			op.State = work.OperationConfirmed
			op.ExternalReceipt = outbox.ExternalMessageID
		} else {
			op.State = work.OperationUnknown
			if recoveryErr != nil {
				op.LastError = recoveryErr.Error()
			} else {
				op.LastError = "publication still has no actual sent receipt"
			}
		}
		if _, _, err = tx.PutOperation(op, expected); err != nil {
			return err
		}
		if op.State != work.OperationConfirmed {
			return fmt.Errorf("原发送仍待核查，未确认成功")
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, id)
	})
	return errors.Join(recoveryErr, err)
}

// observeTeamPublicationInteraction records a historical card-version receipt
// before ordinary business freshness checks. Observation errors must not widen
// permission or turn an old card into a new approval.
func (a *App) observeTeamPublicationInteraction(ctx context.Context, interaction workstore.HumanInteraction) error {
	viewID, _ := interaction.Envelope.Values["team_view"].(string)
	if viewID == "" || a.teamRuntime == nil {
		return nil
	}
	outboxID := ""
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(viewID)
		if err == workstore.ErrNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(view.OperationID)
		if err != nil {
			return err
		}
		if op.State != work.OperationUnknown && op.State != work.OperationSending {
			return nil
		}
		outboxes, err := tx.ListOutbox(view.ScopeID)
		if err != nil {
			return err
		}
		for _, item := range outboxes {
			if item.OperationID == op.ID && (item.State == work.OutboxUnknown || item.State == work.OutboxSending) {
				outboxID = item.ID
				break
			}
		}
		return nil
	})
	if err != nil || outboxID == "" {
		return err
	}
	err = a.teamRuntime.publisher.ObserveUnknownCardInteraction(ctx, outboxID, interaction.ID)
	if errors.Is(err, workstore.ErrConflict) {
		return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			item, e := tx.GetOutbox(outboxID)
			if e == nil && item.State == work.OutboxSent {
				return nil
			}
			if e != nil {
				return e
			}
			return err
		})
	}
	return err
}
