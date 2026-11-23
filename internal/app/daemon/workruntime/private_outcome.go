package workruntime

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A normal private answer is itself the durable processing result. This does
// not infer task completion from model prose or relax native quiescence proof.
func (c *Coordinator) preparePrivateSuccessReport(tx *workstore.Tx, attempt work.ExecutionAttempt, request InspectionRequest, observation NativeObservation) error {
	if _, err := tx.GetProcessingReport(attempt.ID); err == nil {
		return nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	ids, err := privateOutcomeHumanInputs(tx, attempt, false)
	if err != nil || len(ids) == 0 {
		return err
	}
	stopped, err := tx.PrivateAttemptStopped(attempt)
	if err != nil || stopped {
		return err
	}
	turns, err := tx.ListNativeTurns(attempt.ID)
	if err != nil || len(turns) == 0 {
		return err
	}
	for _, turn := range turns {
		if turn.TerminalStatus != "completed" || turn.TerminalSequence == 0 {
			return nil
		}
	}
	result, err := privateOutcomeFinal(tx, attempt, request)
	if err != nil || result.ID == "" {
		return err
	}
	if !observation.provesStopped(request) {
		return fmt.Errorf("private final waits for complete native, continuation and background quiescence")
	}
	settled, err := privateOutcomeEffectsSettled(tx, attempt, request, observation)
	if err != nil {
		return err
	}
	if !settled {
		return fmt.Errorf("private final waits for unresolved native input or business effects")
	}
	context, err := tx.GetContext(attempt.ContextID)
	if err != nil {
		return err
	}
	return tx.PutProcessingReport(workstore.ProcessingReport{AttemptID: attempt.ID, RunRef: attempt.RunRef, ProcessedInputIDs: ids, OutcomeRef: result.ID, Checkpoint: checkpointWithOutcome(context.Checkpoint, string(result.Data)), WorkDisposition: "unknown", Revision: 1}, 0)
}

func privateOutcomeHumanInputs(tx *workstore.Tx, attempt work.ExecutionAttempt, allowControl bool) ([]string, error) {
	scope, err := tx.GetScope(attempt.ScopeID)
	if err != nil || scope.Kind != work.ScopePersonal {
		return nil, err
	}
	context, err := tx.GetContext(attempt.ContextID)
	if err != nil {
		return nil, err
	}
	if attempt.TaskID != "" || context.TaskID != "" || context.Purpose == work.PurposeReview || context.Purpose == work.PurposeControl && !allowControl {
		return nil, nil
	}
	bot, err := tx.GetRoleBotBinding(scope.RoleID)
	if err != nil {
		return nil, err
	}
	command, err := tx.GetNativeCommand(attempt.CommandID)
	if err != nil {
		return nil, err
	}
	if command.Binding.BotGeneration != bot.IdentityGeneration || command.Binding.RoleID != scope.RoleID || command.Binding.ScopeID != scope.ID || command.Binding.ContextID != context.ID || command.Binding.AttemptID != attempt.ID || command.Binding.RunRef != attempt.RunRef.RunID {
		return nil, nil
	}
	ids, err := tx.EffectiveAttemptInputIDs(attempt.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		input, err := tx.GetInbox(id)
		if err != nil {
			return nil, err
		}
		if input.State != work.InboxClaimed || input.ClaimedAttemptID != attempt.ID || input.ScopeID != scope.ID || input.RecipientContextID != context.ID || input.RecipientRoleID != scope.RoleID || input.MessageID == "" || input.SessionGoalIntentID != "" || input.AutomationEpisodeID != "" || input.ControlOfInputID != "" && !allowControl {
			return nil, nil
		}
		message, err := tx.GetMessage(input.MessageID)
		if err != nil {
			return nil, err
		}
		if message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) || message.ScopeID != scope.ID {
			return nil, nil
		}
		authenticated := false
		if message.PlatformKey != nil {
			for _, observation := range message.Observations {
				if observation.AppID == bot.AppID && observation.IdentityGeneration == bot.IdentityGeneration {
					authenticated = true
				}
			}
		} else if message.DerivedFrom != nil && message.DerivedFrom.Kind == "human_interaction" {
			action, err := tx.GetHumanInteraction(message.DerivedFrom.ID)
			if err != nil {
				return nil, err
			}
			authenticated = action.PrincipalID == scope.PrincipalID && action.Gateway.AppID == bot.AppID && action.Gateway.GatewayRef == bot.GatewayRef && action.Gateway.BotGeneration == bot.IdentityGeneration && action.Gateway.RoleID == bot.RoleID && action.Envelope.ChatID != "" && action.Envelope.Tenant != ""
		}
		if !authenticated {
			return nil, nil
		}
	}
	return ids, nil
}

func privateOutcomeFinal(tx *workstore.Tx, attempt work.ExecutionAttempt, request InspectionRequest) (work.Content, error) {
	operations, err := tx.PrivateRunPublications(attempt.ScopeID, attempt.ContextID, attempt.RunRef.RunID)
	if err != nil {
		return work.Content{}, err
	}
	var fallback work.Content
	for _, operation := range operations {
		if operation.Actor != (work.Actor{Kind: work.ActorSystem, ID: "private-execution-display"}) {
			continue
		}
		projection, err := workservice.ReadMessageProjection(tx, operation)
		if err != nil {
			return work.Content{}, err
		}
		if projection.PrivateDisplay == nil {
			continue
		}
		display, err := tx.VerifyPrivateDisplayPublication(operation)
		if err != nil {
			return work.Content{}, err
		}
		if display.Binding != request.Binding || display.Incarnation != request.Carrier.Incarnation || display.SessionID != request.SessionID || display.TurnID != request.TurnID || !display.Terminated || display.RejectionCommandID != "" {
			continue
		}
		if projection.PrivateDisplay.ResultBodyRef != "" && projection.PrivateDisplay.ResultHash != "" {
			content, err := tx.GetContent(attempt.ScopeID, projection.PrivateDisplay.ResultBodyRef)
			if err != nil {
				return work.Content{}, err
			}
			if strings.TrimSpace(string(content.Data)) != "" && workservice.PrivateResultHash(string(content.Data)) == projection.PrivateDisplay.ResultHash {
				return content, nil
			}
		}
		summary := ""
		if media := projection.Media; media != nil && media.Kind == "image" {
			content, err := tx.GetContent(attempt.ScopeID, media.ContentRef)
			if err != nil {
				return work.Content{}, err
			}
			if content.SHA256 != media.SHA256 || len(content.Data) == 0 || !strings.HasPrefix(content.MediaType, "image/") {
				return work.Content{}, workstore.ErrScopeDenied
			}
			summary = "本轮原生执行正常结束，已返回图片。已保存图片：" + content.ID + "；SHA256：" + content.SHA256
		} else if !display.HadOutput && bytes.Equal(projection.Card, feishu.PrivateExecutionNotice("执行已结束", "本轮没有返回可显示的答复。")) {
			summary = "本轮原生执行正常结束，没有返回可显示的答复。"
		} else if bytes.Equal(projection.Card, feishu.PrivateExecutionNotice("执行已结束", "本轮没有返回最终答复。已返回的阶段内容仍然保留。")) {
			summary = "本轮原生执行正常结束，没有返回最终答复；已返回的阶段内容仍然保留。"
		}
		if summary != "" {
			fallback = work.NewContent("private-native-result:"+attempt.ID, attempt.ScopeID, "text/plain", []byte(summary), operation.CreatedAt)
		}
	}
	if fallback.ID != "" {
		return fallback, tx.PutContent(fallback)
	}
	return privateOutcomeExplicitFinal(tx, attempt, request, operations)
}

func privateOutcomeEffectsSettled(tx *workstore.Tx, attempt work.ExecutionAttempt, request InspectionRequest, observation NativeObservation) (bool, error) {
	admissions, err := tx.ListAttemptInputAdmissions(attempt.ID)
	if err != nil {
		return false, err
	}
	for _, admission := range admissions {
		if admission.State != work.InputAdmissionConfirmed && admission.State != work.InputAdmissionRejected && admission.State != work.InputAdmissionNotSent {
			return false, nil
		}
	}
	operations, err := tx.ListOperations(attempt.ScopeID)
	if err != nil {
		return false, err
	}
	for _, operation := range operations {
		if operation.ContextID != attempt.ContextID || operation.ID == attempt.CommandID {
			continue
		}
		switch operation.State {
		case work.OperationPrepared, work.OperationSending, work.OperationUnknown, work.OperationRetryable:
		default:
			continue
		}
		transportOnly, err := privateOutcomeTransportOnly(tx, operation)
		if err != nil {
			return false, err
		}
		if transportOnly {
			continue
		}
		retiredStop, err := privateOutcomeRetiredShutdown(tx, operation, request, observation)
		if err != nil {
			return false, err
		}
		if retiredStop {
			continue
		}
		if operation.Kind == "feishu.publish" && operation.Actor == (work.Actor{Kind: work.ActorSystem, ID: "private-execution-display"}) {
			if _, err := tx.VerifyPrivateDisplayPublication(operation); err != nil {
				return false, err
			}
			continue
		}
		return false, nil
	}
	return true, nil
}
