package workingress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"time"
)

func (i *Ingress) process(ctx context.Context, record work.IngressRecord, expanded ExpandedMessage) (state work.IngressState, partial bool, err error) {
	recalled := false
	blockedReply := ""
	err = i.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		recalled, e = tx.IsPlatformMessageRecalled(record.Envelope.Tenant, record.Envelope.ChatID, record.Envelope.MessageID)
		return e
	})
	if err != nil {
		return state, false, err
	}
	leader, mentions := "", []string{}
	if record.Envelope.SenderType == "user" && !recalled {
		leader, mentions, err = i.directoryTargets(ctx, record)
		if err != nil {
			return state, false, err
		}
	}
	err = i.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetIngress(record.ID)
		if e != nil {
			return e
		}
		if current.State != work.IngressPending {
			return nil
		}
		if current.Revision != record.Revision {
			return workstore.ErrConflict
		}
		next := current
		next.Revision++
		next.LastError = ""
		next.NextAttemptAt = nil
		recalled, e = tx.IsPlatformMessageRecalled(record.Envelope.Tenant, record.Envelope.ChatID, record.Envelope.MessageID)
		if e != nil {
			return e
		}
		if record.Envelope.SenderType != "user" || recalled {
			next.State = work.IngressIgnored
			state = next.State
			return tx.PutIngress(next, current.Revision)
		}
		if record.ReplyDeliveryError != "" {
			return &PrivateReplyRoutingError{Reason: record.ReplyDeliveryError}
		}
		quoted := record.Envelope.ParentID != "" || record.Envelope.RootID != "" || record.Envelope.ThreadID != ""
		if quoted && record.Route.Kind == work.ScopePersonal && record.Route.ProjectViewScopeID != "" {
			personal, e := tx.GetScope(record.Route.ScopeID)
			if e != nil {
				return e
			}
			if e = tx.RequireProjectPrincipalAccess(record.Route.ProjectViewScopeID, personal.PrincipalID, i.config.Now().UTC()); e != nil {
				if !errors.Is(e, workstore.ErrScopeDenied) && !errors.Is(e, workstore.ErrNotFound) {
					return e
				}
				blockedReply = "这条引用属于之前的项目，但处理时项目访问资格已过期或撤销。回复已保存，尚未交给 Agent；请重新进入该项目后继续。"
				next.ReplyDeliveryError = blockedReply
				next.LastError = blockedReply
				state = next.State
				return tx.PutIngress(next, current.Revision)
			}
		}
		if record.Route.ReplyRoutingError != "" {
			return &PrivateReplyRoutingError{Reason: record.Route.ReplyRoutingError}
		}
		principal, _, e := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: record.Envelope.Tenant, AppID: record.Envelope.AppID, Generation: record.Gateway.BotGeneration, UnionDomain: record.Gateway.UnionDomain, IDs: record.Envelope.SenderIDs, ObservationRef: record.ID})
		if e != nil {
			return e
		}
		scope, e := i.resolveScope(tx, record, principal, leader)
		if e != nil {
			return e
		}
		text := expanded.Text
		commandText := text
		if record.Envelope.MessageType == "text" {
			var literal struct {
				Text string `json:"text"`
			}
			if json.Unmarshal([]byte(record.Envelope.RawContent), &literal) == nil {
				commandText = literal.Text
			}
		}
		partial = !expanded.Complete
		if record.Envelope.ChatType == "p2p" && strings.HasPrefix(strings.TrimSpace(commandText), "/") {
			raw, _ := json.Marshal(record.Envelope)
			command := work.TypedCommandEnvelope{ContextID: record.Route.ContextID, NavigationRevision: record.Route.NavigationRevision, ProjectViewScopeID: record.Route.ProjectViewScopeID, ProjectContextID: record.Route.ProjectContextID, ID: opaque("private-command", record.ID), IngressID: record.ID, ScopeID: scope.ID, PrincipalID: principal.ID, RoleID: record.Gateway.RoleID, GatewayRef: record.Gateway.GatewayRef, Text: commandText, Raw: raw, State: "pending", Revision: 1}
			if e = tx.PutPrivateCommand(command, 0); e != nil {
				return e
			}
			next.State = work.IngressCommand
			state = next.State
			return tx.PutIngress(next, current.Revision)
		}
		if !expanded.Complete && !strings.Contains(text, "部分内容未获取") {
			text += "\n\n[部分内容未获取] " + strings.Join(expanded.Issues, "；")
		}
		recipient, e := i.recipient(tx, scope, record.Envelope, mentions)
		if e != nil {
			return e
		}
		if recipient.Note != "" {
			text += "\n\n[归属说明] " + recipient.Note
		}
		body := work.NewContent(opaque("message-body", record.ID), scope.ID, "text/plain; charset=utf-8", []byte(text), i.config.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		message := work.Message{ID: opaque("message", record.ID), ScopeID: scope.ID, ConversationID: opaque("conversation", record.Envelope.Tenant, record.Envelope.ChatID, recipient.RootMessageID), Source: work.Actor{Kind: work.ActorPrincipal, ID: principal.ID}, PlatformKey: &work.MessageKey{Tenant: record.Envelope.Tenant, ChatID: record.Envelope.ChatID, MessageID: record.Envelope.MessageID}, BodyRef: body.ID, Revision: 1, CreatedAt: record.Envelope.ReceivedAt}
		for index, asset := range expanded.Attachments {
			if len(asset.Data) > 10<<20 {
				return fmt.Errorf("attachment exceeds durable limit")
			}
			data := work.NewContent(opaque("attachment", record.ID, fmt.Sprint(index)), scope.ID, asset.MediaType, asset.Data, i.config.Now().UTC())
			if e = tx.PutContent(data); e != nil {
				return e
			}
			message.Attachments = append(message.Attachments, work.MessageAttachment{ContentRef: data.ID, SHA256: data.SHA256, Size: int64(len(data.Data)), MediaType: asset.MediaType, FileName: asset.FileName, SourceMessageID: asset.SourceMessageID, Kind: asset.Kind})
		}
		observations, e := tx.ListIngressObservations(record.ID)
		if e != nil {
			return e
		}
		for _, observation := range observations {
			message.Observations = append(message.Observations, work.Observation{AppID: observation.Gateway.AppID, IdentityGeneration: observation.Gateway.BotGeneration, ReceivedAt: observation.Envelope.ReceivedAt})
		}
		contentState := work.InputContentReady
		if partial {
			contentState = work.InputContentPartial
		}
		input := work.Inbox{ID: opaque("inbox", record.ID, recipient.RoleID), ScopeID: scope.ID, TaskID: recipient.TaskID, RelatedTaskID: recipient.RelatedTaskID, RecipientRoleID: recipient.RoleID, EventID: "message:" + message.ID, MessageID: message.ID, State: work.InboxPending, ContentState: contentState, AvailableAt: record.Envelope.ReceivedAt, Revision: 1}
		if recipient.DirectedReplyBotGeneration > 0 {
			input.DirectedReplyIngressID = record.ID
			input.DirectedReplyBotGeneration = recipient.DirectedReplyBotGeneration
		}
		if scope.Kind == work.ScopePersonal {
			input.RecipientContextID = record.Route.ContextID
		}
		if _, _, e = tx.AcceptMessage(message, []work.Inbox{input}); e != nil {
			return e
		}
		if scope.Kind == work.ScopePersonal && i.config.QueuePrivateExecution != nil {
			if e = i.config.QueuePrivateExecution(tx, scope, input, record, i.config.Now().UTC()); e != nil {
				return e
			}
		}
		if e = tx.OpenLegacyPrivateStopForInput(scope, input, message); e != nil {
			return e
		}
		if e = i.controlCopy(tx, scope, input); e != nil {
			return e
		}
		mapping := work.PlatformMessageMapping{Tenant: record.Envelope.Tenant, ChatID: record.Envelope.ChatID, MessageID: record.Envelope.MessageID, ScopeID: scope.ID, TaskID: recipient.TaskID, RootMessageID: recipient.RootMessageID, ChannelGeneration: record.Route.BindingGeneration, IngressID: record.ID}
		if mapping.TaskID == "" {
			mapping.TaskID = recipient.RelatedTaskID
		}
		if scope.Kind == work.ScopeProject {
			// A replay retains its receive-time project destination, but an obsolete
			// channel observation cannot grant fresh access to either project.
			binding, lookup := tx.GetProjectChannelBinding(record.Envelope.Tenant, record.Envelope.ChatID)
			if lookup != nil && !errors.Is(lookup, workstore.ErrNotFound) {
				return lookup
			}
			if lookup == nil && binding.Active && binding.ProjectID == scope.ProjectID && binding.BindingGeneration == record.Route.BindingGeneration {
				if e = tx.GrantProjectAccessFromIngress(scope.ID, principal.ID, record.ID, record.Envelope.MembershipObservedAt().Add(time.Hour)); e != nil {
					return e
				}
			}
		}
		if e = tx.PutPlatformMessageMapping(mapping); e != nil {
			return e
		}
		next.State = work.IngressProcessed
		next.MessageID = message.ID
		state = next.State
		return tx.PutIngress(next, current.Revision)
	})
	return
}
func (i *Ingress) deferFailure(ctx context.Context, id string, cause error) error {
	return i.store.Within(ctx, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(id)
		if err != nil {
			return err
		}
		if record.State != work.IngressPending {
			return nil
		}
		expected := record.Revision
		record.Revision++
		record.LastError = cause.Error()
		next := i.config.Now().Add(time.Minute)
		record.NextAttemptAt = &next
		return tx.PutIngress(record, expected)
	})
}
func (i *Ingress) DispatchPrivateCommands(ctx context.Context, limit int) (int, error) {
	if i.config.PrivateCommands == nil {
		return 0, nil
	}
	if limit < 1 || limit > 100 {
		return 0, fmt.Errorf("command limit must be 1..100")
	}
	var commands []work.TypedCommandEnvelope
	err := i.store.Within(ctx, func(tx *workstore.Tx) error { var e error; commands, e = tx.ListPendingPrivateCommands(); return e })
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, command := range commands {
		if processed == limit {
			break
		}
		err = i.store.Within(ctx, func(tx *workstore.Tx) error {
			current, e := tx.GetPrivateCommand(command.ID)
			if e != nil {
				return e
			}
			if current.State != "pending" {
				return workstore.ErrConflict
			}
			expected := current.Revision
			current.Revision++
			current.State = "sending"
			command = current
			return tx.PutPrivateCommand(current, expected)
		})
		if errors.Is(err, workstore.ErrConflict) {
			continue
		}
		if err != nil {
			return processed, err
		}
		receipt, sendErr := i.config.PrivateCommands.Handle(ctx, command)
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = i.store.Within(saveCtx, func(tx *workstore.Tx) error {
			current, e := tx.GetPrivateCommand(command.ID)
			if e != nil {
				return e
			}
			expected := current.Revision
			current.Revision++
			current.State = "handled"
			current.Receipt = receipt.Receipt
			if receipt.Rejected && receipt.Receipt != "" && !receipt.Unknown {
				current.LastError = receipt.ErrorDetail
				if current.LastError == "" && sendErr != nil {
					current.LastError = sendErr.Error()
				}
			} else if sendErr != nil || receipt.Unknown || receipt.Receipt == "" {
				current.State = "unknown"
				if sendErr != nil {
					current.LastError = sendErr.Error()
				}
			}
			return tx.PutPrivateCommand(current, expected)
		})
		cancel()
		if err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// RetryIngress is a trusted recovery trigger after an explicit identity or
// leader choice was saved. It changes no frozen receive-time routing facts.
func (i *Ingress) RetryIngress(ctx context.Context, id string) error {
	return i.store.Within(ctx, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(id)
		if err != nil {
			return err
		}
		if record.State != work.IngressPending {
			return nil
		}
		expected := record.Revision
		record.Revision++
		record.NextAttemptAt = nil
		record.LastError = ""
		return tx.PutIngress(record, expected)
	})
}
