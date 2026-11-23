package workrequests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func readForm(tx *workstore.Tx, request workstore.NativeRequest) (orchestrator.RequestFormState, error) {
	var form orchestrator.RequestFormState
	body, err := tx.GetContent(request.ScopeID, request.FormRef)
	if err != nil {
		return form, err
	}
	err = json.Unmarshal(body.Data, &form)
	return form, err
}

func queueForm(tx *workstore.Tx, request workstore.NativeRequest, form orchestrator.RequestFormState, interactionIDs ...string) error {
	viewID := workui.ID(request.ID, strconv.FormatInt(request.Revision, 10))
	var response work.Operation
	if request.State == "response_pending" && request.ResponseOperationID != "" {
		var err error
		response, err = tx.GetOperation(request.ResponseOperationID)
		if err != nil {
			return err
		}
		viewID = workui.ID(request.ID, strconv.FormatInt(request.Revision, 10), "response", response.ID, strconv.FormatInt(response.Revision, 10))
	}
	view, blocked, err := requestPresentationDestination(tx, request, viewID)
	if err != nil || blocked {
		return err
	}
	view.ID = viewID
	if view.ReplacesViewID == view.ID {
		return nil
	}
	view.SurfaceKey = "request:" + request.ID
	view.Kind = "native.request"
	view.TargetID = request.ID
	view.TargetRevision = request.Revision
	view.ContextID = request.Binding.ContextID
	var card json.RawMessage
	if request.State == "open" {
		card, err = workui.RequestFormCard(view.ID, form)
	} else {
		text := "答复正在交给 Agent，收到原生确认后会更新。"
		if request.State == "response_pending" {
			switch response.State {
			case work.OperationRetryable:
				text = "已保存你的选择，但尚未送到后端，系统会自动重试。"
				if response.LastError != "" {
					detail := []rune(response.LastError)
					if len(detail) > 400 {
						detail = detail[:400]
					}
					text += "\n原因：" + string(detail)
				}
			case work.OperationUnknown:
				text = "已保存你的选择，尚未确认是否送达。系统正在核对，不会重复发送。"
			}
		}
		if request.State == "resolved" {
			text = "这项请求已结束。"
		} else if request.State == "stale" {
			text = "这项请求对应的执行或内容已变化，旧答复不会再被采用。"
		}
		if request.ResolvedEvidenceRef != "" {
			evidence, e := tx.GetContent(request.ScopeID, request.ResolvedEvidenceRef)
			if e != nil {
				return e
			}
			if evidence.MediaType == retiredControlRequestEvidence {
				text = "原执行器已退出，取消未送达；这项请求已关闭。原执行状态仍等待核对。"
			} else if evidence.MediaType == workstore.NativeRequestTerminalEvidenceType {
				text = "执行已结束，这项原生请求不再等待答复。已有选择和发送记录已保留。"
			}
		}

		card, err = workui.Card(view.ID, workui.Panel{Title: form.Title, Paragraphs: []string{text}})
	}
	if err != nil {
		// Preserve the native request instead of guessing an answer or silently
		// dropping a form that exceeds Feishu's presentation constraints.
		card, err = workui.Card(view.ID, workui.Panel{Title: "这项原生请求需要另行处理", Paragraphs: []string{"当前请求不能在飞书中完整显示，系统已保留原始请求，不会自动批准。可以先暂停这项工作，再处理原生请求。"}})
		if err != nil {
			return err
		}
	}
	if len(interactionIDs) > 0 {
		if err = workui.ReplaceFromInteraction(tx, &view, interactionIDs[0]); err != nil {
			return err
		}
	}
	_, err = workui.Queue(tx, view, card)
	return err
}

func (s *Service) ProjectForms(ctx context.Context) error {
	recoveryErr := s.RecoverUnsentResponses(ctx, 32)
	var ids []string
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, e := tx.ListScopes()
		if e != nil {
			return e
		}
		for _, scope := range scopes {
			requests, e := tx.ListNativeRequests(scope.ID)
			if e != nil {
				return e
			}
			for _, request := range requests {
				if request.PromptRef != "" {
					ids = append(ids, request.ID)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	if recoveryErr != nil {
		failures = append(failures, recoveryErr)
	}
	for _, id := range ids {
		if err := s.projectForm(ctx, id); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func (s *Service) projectForm(ctx context.Context, id string) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		request, err := tx.GetNativeRequest(id)
		if err != nil {
			return err
		}
		if ended, e := tx.CloseNativeRequestForTerminalTurn(id, time.Now().UTC()); e != nil {
			return e
		} else if ended {
			request, e = tx.GetNativeRequest(id)
			if e != nil {
				return e
			}
		}
		if request.State == "open" || request.State == "response_pending" {
			worker, e := tx.GetWorker(request.Binding.WorkerSessionID)
			if e != nil {
				return e
			}
			carrier, e := tx.GetCarrier(request.CarrierIncarnation)
			if e != nil {
				return e
			}
			bot, e := tx.GetRoleBotBinding(worker.RoleID)
			if e != nil {
				return e
			}
			if !carrier.Matches(worker) || worker.Controller != work.ControllerAutomatic || bot.IdentityGeneration != request.Binding.BotGeneration {
				previous := request.Revision
				request.Revision++
				request.State = "stale"
				request.UpdatedAt = time.Now().UTC()
				if e = tx.PutNativeRequest(request, previous); e != nil {
					return e
				}
			}
		}
		var e error
		var form orchestrator.RequestFormState
		if request.FormRef == "" {
			body, e := tx.GetContent(request.ScopeID, request.PromptRef)
			if e != nil {
				return e
			}
			var event agentproto.Event
			if e = json.Unmarshal(body.Data, &event); e != nil {
				return e
			}
			carrier, e := tx.GetCarrier(request.CarrierIncarnation)
			if e != nil {
				return e
			}
			worker, e := tx.GetWorker(request.Binding.WorkerSessionID)
			if e != nil {
				return e
			}
			location, e := tx.GetLocation(worker.LocationID)
			if e != nil {
				return e
			}
			form, e = orchestrator.NewRequestForm(agentproto.Backend(carrier.Backend), event.RequestPrompt, event.Metadata, location.Cwd)
			if e != nil {
				return queueUnrenderable(tx, request, e)
			}
			encoded, e := json.Marshal(form)
			if e != nil {
				return e
			}
			content := work.NewContent(request.ID+":form:1", request.ScopeID, "application/json", encoded, time.Now().UTC())
			if e = tx.PutContent(content); e != nil {
				return e
			}
			previous := request.Revision
			request.Revision++
			request.FormRef = content.ID
			if e = tx.PutNativeRequest(request, previous); e != nil {
				return e
			}
		} else {
			form, e = readForm(tx, request)
			if e != nil {
				return e
			}
		}
		if e = queueForm(tx, request, form); e != nil {
			return e
		}
		return nil
	})
}

// HandleForm applies a durable, authenticated SDK action to its exact request
// revision. Multi-step edits are local; native responses use Respond's journal.
func (s *Service) HandleForm(ctx context.Context, interactionID, viewID string) error {
	var interaction workstore.HumanInteraction
	var request workstore.NativeRequest
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		interaction, e = tx.GetHumanInteraction(interactionID)
		if e != nil {
			return e
		}
		view, e := tx.GetUIView(viewID)
		if e != nil {
			return e
		}
		request, e = tx.GetNativeRequest(view.TargetID)
		return e
	})
	if err != nil {
		return err
	}
	decision := Decision{RequestID: request.ID, PrincipalID: interaction.PrincipalID, EvidenceRef: interaction.ID}
	if err = s.authorize(ctx, request, decision); err != nil {
		return err
	}
	complete := false
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		if saved, e := tx.GetContent(request.ScopeID, interaction.ID+":decision"); e == nil {
			complete = true
			return json.Unmarshal(saved.Data, &decision)
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		view, e := tx.GetUIView(viewID)
		if e != nil {
			return e
		}
		if view.Kind != "native.request" {
			return fmt.Errorf("view is not a native request")
		}
		if e = tx.ValidateViewInteraction(view, interaction); e != nil {
			return e
		}
		fresh, e := tx.GetNativeRequest(view.TargetID)
		if e != nil {
			return e
		}
		if fresh.Revision != view.TargetRevision || fresh.State != "open" {
			return fmt.Errorf("request has changed; use its current form")
		}
		form, e := readForm(tx, fresh)
		if e != nil {
			return e
		}
		action, e := formAction(interaction.Envelope, form)
		if e != nil {
			return e
		}
		transition, e := orchestrator.TransformRequestForm(form, action)
		if e != nil {
			return e
		}
		evidence, e := json.Marshal(interaction)
		if e != nil {
			return e
		}
		if e = tx.PutContent(work.NewContent(interaction.ID, fresh.ScopeID, "application/json", evidence, time.Now().UTC())); e != nil {
			return e
		}
		if !transition.Complete {
			body, e := json.Marshal(transition.NextForm)
			if e != nil {
				return e
			}
			content := work.NewContent(interaction.ID+":form", fresh.ScopeID, "application/json", body, time.Now().UTC())
			if e = tx.PutContent(content); e != nil {
				return e
			}
			previous := fresh.Revision
			fresh.Revision++
			fresh.FormRef = content.ID
			if e = tx.PutNativeRequest(fresh, previous); e != nil {
				return e
			}
			if e = queueForm(tx, fresh, transition.NextForm, interaction.ID); e != nil {
				return e
			}
			return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, content.ID)
		}
		bridge := control.ResolveRequestBridgeContract(form.SemanticKind, form.RequestType)
		decision.ExpectedRevision = fresh.Revision
		decision.FollowupHumanInput = transition.FollowupHumanInput
		decision.Response = agentproto.Request{RequestID: fresh.NativeRequestID, Response: transition.Response, BridgeKind: string(bridge.Kind), SemanticKind: control.NormalizeRequestSemanticKind(form.SemanticKind, form.RequestType), InterruptOnDecline: control.RequestBridgeShouldInterruptOnDecline(bridge, transition.Response)}
		body, e := json.Marshal(decision)
		if e != nil {
			return e
		}
		if e = tx.PutContent(work.NewContent(interaction.ID+":decision", fresh.ScopeID, "application/json", body, time.Now().UTC())); e != nil {
			return e
		}
		complete = true
		return nil
	})
	if err != nil || !complete {
		return err
	}
	opID, sendErr := s.Respond(ctx, decision)
	finishErr := s.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
		if _, e := tx.GetOperation(opID); e != nil {
			return e
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, opID)
	})
	return errors.Join(sendErr, finishErr)
}

func formAction(raw work.HumanInteractionEnvelope, form orchestrator.RequestFormState) (orchestrator.HumanRequestFormAction, error) {
	a := orchestrator.HumanRequestFormAction{Answers: map[string][]string{}}
	a.OptionID, _ = raw.Values["option_id"].(string)
	a.SkipQuestionID, _ = raw.Values["skip_question_id"].(string)
	for id, value := range raw.Form {
		if id == "__feedback" && form.FeedbackMode != "" {
			text, ok := value.(string)
			if !ok {
				return a, fmt.Errorf("invalid feedback value")
			}
			a.Feedback = text
			continue
		}
		switch v := value.(type) {
		case string:
			a.Answers[id] = []string{v}
		case []string:
			a.Answers[id] = v
		case []any:
			for _, item := range v {
				str, ok := item.(string)
				if !ok {
					return a, fmt.Errorf("invalid form answer")
				}
				a.Answers[id] = append(a.Answers[id], str)
			}
		default:
			return a, fmt.Errorf("unsupported form field value")
		}
	}
	return a, nil
}

func saveFollowup(tx *workstore.Tx, request workstore.NativeRequest, interaction workstore.HumanInteraction, text string) error {
	id := interaction.ID + ":followup"
	content := work.NewContent(id+":body", request.ScopeID, "text/plain", []byte(text), time.Now().UTC())
	if err := tx.PutContent(content); err != nil {
		return err
	}
	message := work.Message{ID: id, ScopeID: request.ScopeID, ConversationID: request.Binding.ContextID, Source: work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}, BodyRef: content.ID, DerivedFrom: &work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: strconv.FormatInt(interaction.Revision, 10), EvidenceRef: interaction.ID}, Revision: 1, CreatedAt: time.Now().UTC()}
	_, _, err := tx.AcceptMessage(message, []work.Inbox{{ID: id + ":inbox", ScopeID: request.ScopeID, RecipientRoleID: request.Binding.RoleID, RecipientContextID: request.Binding.ContextID, EventID: id, MessageID: id, State: work.InboxPending, Revision: 1}})
	return err
}
