package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workrequests"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) receiveTeamInteraction(ctx context.Context, raw work.HumanInteractionEnvelope) (string, error) {
	r := a.teamRuntime
	if r == nil {
		return "", fmt.Errorf("team runtime unavailable")
	}
	gateway, err := (teamDirectory{app: a, store: r.store}).ResolveGateway(ctx, raw.GatewayRef, raw.AppID)
	if err != nil {
		return "", err
	}
	var interaction workstore.HumanInteraction
	rejection := ""
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		interaction, _, e = tx.AcceptHumanInteraction(raw, gateway)
		if e != nil {
			return e
		}
		if id, _ := raw.Values["team_view"].(string); id == "" && raw.Values["team_setup_ingress"] == nil && interaction.State == "pending" {
			return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, "legacy_view_retired")
		}
		if viewID, _ := raw.Values["team_view"].(string); viewID != "" && interaction.State == "pending" {
			view, e := tx.GetUIView(viewID)
			if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			if e != nil || tx.ValidateViewInteraction(view, interaction) != nil {
				rejection = "这张卡片已过期或不属于当前对话，请重新打开对应入口。"
				// Keep a matching unknown publication callback pending for read-only
				// receipt reconciliation. Its obsolete action still cannot execute.
				if e == nil && view.Binding.GatewayRef == gateway.GatewayRef && view.Binding.AppID == gateway.AppID && view.Binding.IdentityGeneration == gateway.BotGeneration && view.Tenant == raw.Tenant && view.ChatID == raw.ChatID {
					op, lookupErr := tx.GetOperation(view.OperationID)
					if lookupErr != nil {
						return lookupErr
					}
					if op.State == work.OperationUnknown || op.State == work.OperationSending {
						if !view.Invalidated && view.ExternalMessageID == "" && (view.PrincipalID == "" || view.PrincipalID == interaction.PrincipalID) {
							rejection = "已收到，正在核对这张卡片的发送结果；核对完成后再处理这次操作。"
						}
						return nil
					}
				}
				return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, "obsolete_view")
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if rejection != "" {
		return rejection, nil
	}
	if id, _ := raw.Values["team_view"].(string); id == "" && raw.Values["team_setup_ingress"] == nil {
		return "旧界面已停用。群里可以直接告诉负责人；私聊可发送 /menu。", nil
	}
	if interaction.State == "handled" {
		return "这次操作已经处理。", nil
	}
	return "已收到，正在处理。", nil
}

func (a *App) authorizeNativeRequest(ctx context.Context, request workstore.NativeRequest, decision workrequests.Decision) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, err := tx.GetHumanInteraction(decision.EvidenceRef)
		if err != nil {
			return err
		}
		if interaction.PrincipalID != decision.PrincipalID {
			return workstore.ErrScopeDenied
		}
		viewID, _ := interaction.Envelope.Values["team_view"].(string)
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		if view.Kind != "native.request" || view.TargetID != request.ID || view.ScopeID != request.ScopeID {
			return workstore.ErrScopeDenied
		}
		// A response already durably accepted may be retried as a lookup after
		// its old card was replaced. No new response can pass Respond's CAS.
		if request.ResponseOperationID != "" {
			op, err := tx.GetOperation(request.ResponseOperationID)
			if err != nil {
				return err
			}
			if op.Actor == (work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}) {
				return nil
			}
		}
		return tx.ValidateViewInteraction(view, interaction)
	})
}

func (a *App) processTeamInteractions(ctx context.Context) error {
	r := a.teamRuntime
	var pending []workstore.HumanInteraction
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		pending, e = tx.ListPendingHumanInteractionHeads(32)
		return e
	}); err != nil {
		return err
	}
	var failures []error
	for _, interaction := range pending {
		if r.runContext != nil {
			r.queueHumanAction(interaction, a.processTeamInteraction)
			continue
		}
		if err := a.processTeamInteraction(ctx, interaction); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (a *App) processTeamInteraction(ctx context.Context, interaction workstore.HumanInteraction) error {
	r := a.teamRuntime
	var failures []error
	if interaction.Envelope.Values["team_setup_ingress"] != nil {
		if err := a.handleTeamLeaderSelection(ctx, interaction); err != nil {
			failures = append(failures, err)
			if e := a.finishInteractionError(ctx, interaction, workstore.UIView{}, err); e != nil {
				failures = append(failures, e)
			}
		}
		return errors.Join(failures...)
	}
	if e := a.observeTeamPublicationInteraction(ctx, interaction); e != nil {
		log.Printf("team publication observation %s: %v", interaction.ID, e)
	}
	viewID, _ := interaction.Envelope.Values["team_view"].(string)
	action, _ := interaction.Envelope.Values["action"].(string)
	var view workstore.UIView
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		view, e = tx.GetUIView(viewID)
		if e != nil {
			return e
		}
		return tx.ValidateViewInteraction(view, interaction)
	})
	if err == nil {
		switch {
		case view.Kind == "review.result":
			err = a.handleTeamReviewView(ctx, interaction, view, action)
		case strings.HasPrefix(view.Kind, "host.codex-upgrade."):
			err = a.handleTeamCodexUpgradeView(ctx, interaction, view, action)
		case view.Kind == "project.directory" || (view.Kind == "project.status" && action == "project.directory.open"):
			err = a.handleTeamProjectDirectory(ctx, interaction, view, action)
		case view.Kind == "publication.recovery":
			err = a.handleTeamPublicationRecovery(ctx, interaction, view, action)
		case view.Kind == "resource.write.recovery" || view.Kind == "resource.write.recovery.confirm":
			err = a.handleTeamResourceWriteRecovery(ctx, interaction, view, action)
		case view.Kind == "project.recovery":
			err = a.handleTeamProvisionRecovery(ctx, interaction, view, action)
		case view.Kind == "business.decision" || view.Kind == "project.artifacts":
			err = a.handleTeamBusinessView(ctx, interaction, view, action)
		case strings.HasPrefix(action, "runtime."):
			err = a.handleTeamRecoveryAction(ctx, interaction, view, action)
		case action == "native.respond":
			err = r.requests.HandleForm(ctx, interaction.ID, viewID)
		case strings.HasPrefix(action, "project."):
			err = a.handleHumanProjectLifecycle(ctx, interaction, view, strings.TrimPrefix(action, "project."))
		default:
			err = a.handleTeamViewAction(ctx, interaction, view, action)
		}
	}
	if err != nil {
		failures = append(failures, err)
		if e := a.finishInteractionError(ctx, interaction, view, err); e != nil {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}

func (a *App) finishInteractionError(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, problem error) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		fresh, err := tx.GetHumanInteraction(interaction.ID)
		if err != nil {
			return err
		}
		if fresh.State == "handled" {
			return nil
		}
		if view.ID != "" && view.ChatID == interaction.Envelope.ChatID && view.Tenant == interaction.Envelope.Tenant && view.Binding.AppID == interaction.Gateway.AppID && (view.PrincipalID == "" || view.PrincipalID == interaction.PrincipalID) {
			// Preserve the original form so a validation error does not erase
			// the user's choices. This notice has no authority-bearing buttons.
			notice := view
			notice.ReplacesViewID = ""
			notice.ID = workui.ID(interaction.ID, "error")
			notice.SurfaceKey = notice.ID
			notice.Kind = "interaction.notice"
			notice.TargetID = interaction.ID
			notice.TargetRevision = 1
			notice.ExternalMessageID = ""
			notice.Invalidated = false
			card, err := workui.Card(notice.ID, workui.Panel{Title: "这次操作没有完成", Paragraphs: []string{problem.Error(), "可以查看当前状态后重新操作；原有工作和资料仍会保留。"}})
			if err != nil {
				return err
			}
			if _, err = workui.Queue(tx, notice, card); err != nil {
				return err
			}
		}
		return tx.FinishHumanInteraction(fresh.ID, fresh.Revision, "not_applied:"+problem.Error())
	})
}

func (a *App) handleHumanProjectLifecycle(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	if view.Kind != "project.status" || (action != "pause" && action != "resume" && action != "archive" && action != "reopen") {
		return fmt.Errorf("project action does not match this view")
	}
	messageID := interaction.ID + ":instruction"
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, e := tx.GetMessage(messageID); e == nil {
			return nil
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		proof, e := json.Marshal(interaction)
		if e != nil {
			return e
		}
		content := work.NewContent(interaction.ID, view.ScopeID, "application/json", proof, time.Now().UTC())
		if e = tx.PutContent(content); e != nil {
			return e
		}
		body := work.NewContent(messageID+":body", view.ScopeID, "text/plain", []byte("Human selected project action: "+action), time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.AcceptMessage(work.Message{ID: messageID, ScopeID: view.ScopeID, ConversationID: view.ChatID, Source: work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}, DerivedFrom: &work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: strconv.FormatInt(interaction.Revision, 10), EvidenceRef: content.ID}, BodyRef: body.ID, Revision: 1, CreatedAt: time.Now().UTC()}, nil)
		return e
	})
	if err != nil {
		return err
	}
	result, err := a.teamRuntime.service.ApplyHumanProjectLifecycle(ctx, interaction.PrincipalID, workservice.HumanProjectLifecycleRequest{Action: action, ProjectLifecycleRequest: workservice.ProjectLifecycleRequest{Key: interaction.ID, ProjectID: view.TargetID, ExpectedRevision: view.TargetRevision, SourceMessageID: messageID}})
	if err != nil {
		return err
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, result.OperationID)
	})
}
