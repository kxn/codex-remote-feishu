package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PublishDeliveryRequest struct {
	Key                string              `json:"key"`
	Purpose            string              `json:"purpose"`
	Summary            string              `json:"summary"`
	NeededAction       string              `json:"needed_action,omitempty"`
	References         []work.DeliveryRef  `json:"references"`
	Audience           []work.Actor        `json:"audience"`
	ConversationTarget string              `json:"conversation_target,omitempty"`
	ReplyToInputID     string              `json:"reply_to_input_id,omitempty"`
	Retention          work.RetentionClass `json:"retention"`
}

func (s *Service) PublishDelivery(ctx context.Context, token, runRef string, req PublishDeliveryRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		id := stableID("delivery", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		var err error
		result, err = s.mutate(tx, c, "delivery.publish", req.Key, id, req, func() (string, int64, error) {
			if len(req.References) == 0 {
				return "", 0, fmt.Errorf("delivery requires an explicit resource manifest")
			}
			projection, err := s.messageDestination(tx, c, req.ReplyToInputID)
			if err != nil {
				return "", 0, err
			}
			if req.ConversationTarget != "" && req.ConversationTarget != projection.ChatID {
				return "", 0, workauth.ErrUnauthorized
			}
			scope, err := tx.GetScope(c.Worker.ScopeID)
			if err != nil {
				return "", 0, err
			}
			for _, actor := range req.Audience {
				switch actor.Kind {
				case work.ActorRole:
					if err := ensureMember(tx, c.Worker.ScopeID, actor.ID); err != nil {
						return "", 0, err
					}
				case work.ActorPrincipal:
					if scope.Kind == work.ScopePersonal {
						if actor.ID != scope.PrincipalID {
							return "", 0, workauth.ErrUnauthorized
						}
					} else {
						messages, err := tx.ListMessages(scope.ID)
						if err != nil {
							return "", 0, err
						}
						observed := false
						for _, message := range messages {
							if message.Source == actor && message.PlatformKey != nil && message.PlatformKey.Tenant == projection.Tenant && message.PlatformKey.ChatID == projection.ChatID {
								observed = true
								break
							}
						}
						if !observed {
							return "", 0, workauth.ErrUnauthorized
						}
					}
				default:
					return "", 0, workauth.ErrUnauthorized
				}
			}
			var navigation strings.Builder
			navigation.WriteString(req.Purpose + "\n" + req.Summary)
			for _, ref := range req.References {
				resource, err := tx.GetResource(c.Worker.ScopeID, ref.ResourceID)
				if err != nil {
					return "", 0, err
				}
				if ref.Current {
					if req.Retention == work.RetentionPermanent {
						return "", 0, fmt.Errorf("permanent delivery needs saved fixed versions, not changing current references")
					}
					navigation.WriteString("\n" + resource.CanonicalURI + " [当前引用，会变化]")
				} else {
					version, err := tx.GetResourceVersion(c.Worker.ScopeID, ref.VersionID)
					if err != nil {
						return "", 0, err
					}
					if version.ResourceID != resource.ID || !version.Complete {
						return "", 0, fmt.Errorf("delivery fixed version must be complete for the selected resource range")
					}
					if req.Retention == work.RetentionPermanent && version.ContentRef == "" {
						return "", 0, fmt.Errorf("permanent delivery requires a retained content snapshot")
					}
					navigation.WriteString("\n原文（可能更新）：" + resource.CanonicalURI + "\n发布时的内容已独立保存，可从成果卡回看。")
				}
			}
			if req.NeededAction != "" {
				navigation.WriteString("\n需要处理：" + req.NeededAction)
			}
			viewID := stableID("delivery-view", id)
			card, err := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"title": cardkit.PlainText(req.Purpose)}, "body": map[string]any{"elements": []any{cardkit.PlainTextBlockElement(navigation.String()), map[string]any{"tag": "button", "text": cardkit.PlainText("查看已保存版本"), "behaviors": []any{map[string]any{"type": "callback", "value": map[string]any{"team_view": viewID, "action": "artifacts.open"}}}}}}})
			if err != nil {
				return "", 0, err
			}
			if len(card) > 28*1024 {
				return "", 0, fmt.Errorf("delivery notice too large; use a concise summary")
			}
			projection.Card = card
			projection.UIViewID = viewID
			projection.Body = navigation.String()
			encoded, err := json.Marshal(projection)
			if err != nil {
				return "", 0, err
			}
			body := work.NewContent(stableID("delivery-body", id), scope.ID, "application/json", encoded, s.now())
			if err := tx.PutContent(body); err != nil {
				return "", 0, err
			}
			notificationID := stableID("delivery-notification", id)
			operation := work.Operation{ID: notificationID, ScopeID: scope.ID, ContextID: c.Worker.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: "feishu.publish", TargetID: projection.ChatID, TargetGeneration: projection.BotGeneration, IdempotencyKey: notificationID, RequestRef: body.ID, RequestHash: stableID("delivery-notification-hash", string(encoded)), State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
			outbox := work.Outbox{ID: stableID("delivery-outbox", id), ScopeID: scope.ID, OperationID: notificationID, Target: projection.ChatID, EventID: id, PayloadRef: body.ID, State: work.OutboxPending, Revision: 1}
			delivery := work.Delivery{ID: id, ScopeID: scope.ID, Audience: req.Audience, Purpose: req.Purpose, Summary: req.Summary, NeededAction: req.NeededAction, References: req.References, Retention: req.Retention, ConversationTarget: projection.ChatID, SourceAttemptID: c.Attempt.ID, NotificationOutboxID: outbox.ID, Revision: 1, CreatedAt: s.now()}
			if err := tx.PublishDelivery(workstore.PublishDeliveryRequest{Delivery: delivery, Operation: operation, Outbox: outbox}); err != nil {
				return "", 0, err
			}
			binding, err := tx.GetRoleBotBinding(c.Worker.RoleID)
			if err != nil {
				return "", 0, err
			}
			principal := ""
			if scope.Kind == work.ScopePersonal {
				principal = scope.PrincipalID
			}
			if err = tx.PutUIView(workstore.UIView{ID: viewID, ScopeID: scope.ID, ContextID: c.Worker.ContextID, SurfaceKey: "delivery:" + id, Kind: "project.artifacts", TargetID: id, TargetRevision: 1, Binding: binding, Tenant: projection.Tenant, ChatID: projection.ChatID, PrincipalID: principal, ChannelGeneration: projection.ChannelGeneration, BodyRef: body.ID, OperationID: operation.ID, Revision: 1, CreatedAt: s.now()}, 0); err != nil {
				return "", 0, err
			}
			return id, 1, nil
		})
		return err
	})
	if err == nil {
		result.Status = "accepted"
	}
	return result, err
}
