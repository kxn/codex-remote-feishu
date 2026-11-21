package workservice

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestBusinessDecisionRequestFreezesChoicesAndWakesResponsibleRole(t *testing.T) {
	s, token, run := knowledgeService(t)
	created, err := s.RequestDecision(t.Context(), token, run, RequestDecisionRequest{Key: "choose", Title: "选择方案", Prompt: "保持兼容还是替换接口？", Choices: []work.DecisionChoice{{Value: "keep", Label: "保持兼容"}, {Value: "replace", Label: "替换接口"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DecisionRespond(t.Context(), token, run, DecisionRespondRequest{Key: "forged", RequestID: created.ObjectID, ExpectedRevision: 1, RequestGeneration: 1, Choice: "not-offered"}); err == nil {
		t.Fatal("invented option accepted")
	}
	response, err := s.DecisionRespond(t.Context(), token, run, DecisionRespondRequest{Key: "keep", RequestID: created.ObjectID, ExpectedRevision: 1, RequestGeneration: 1, Choice: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if response.ObjectID != created.ObjectID {
		t.Fatal("decision applied to another object")
	}
	err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		request, err := tx.GetDecisionRequest("scope", created.ObjectID)
		if err != nil {
			return err
		}
		if request.State != work.DecisionResolved {
			t.Fatal("pure local conclusion not applied")
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		found := false
		for _, input := range inputs {
			if input.EventID != "" && input.MessageID != "human-input" {
				message, err := tx.GetMessage(input.MessageID)
				if err != nil {
					return err
				}
				body, err := tx.GetContent("scope", message.BodyRef)
				if err != nil {
					return err
				}
				if string(body.Data) == "选择方案：keep" {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("responsible role was not woken with actual decision")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestHumanDecisionUsesActualSDKInteractionAndExactView(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false)
	const viewID = "actual-human-decision-card"
	var interaction workstore.HumanInteraction
	var request work.DecisionRequest
	err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		binding, err := tx.GetRoleBotBinding("leader")
		if err != nil {
			return err
		}
		raw := work.HumanInteractionEnvelope{GatewayRef: binding.GatewayRef, AppID: binding.AppID, EventID: "sdk-human-choice", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "actual-human-open-id"}, ChatID: "group", MessageID: "actual-card-message", Kind: "card", Values: map[string]any{"team_view": viewID, "action": "decision.choose", "choice": "approve"}, ReceivedAt: s.now()}
		interaction, _, err = tx.AcceptHumanInteraction(raw, work.IngressGateway{RoleID: binding.RoleID, GatewayRef: binding.GatewayRef, AppID: binding.AppID, BotGeneration: binding.IdentityGeneration})
		if err != nil {
			return err
		}
		identity := work.IdentityScope{Tenant: "tenant", AppIdentityDomain: binding.AppID, IDType: "open_id", ExternalID: "actual-human-open-id", Generation: binding.IdentityGeneration}
		query := workstore.MembershipQuery{ScopeID: "project-scope", PrincipalID: interaction.PrincipalID, Tenant: "tenant", ChatID: "group", BindingGeneration: 1, QueriedIdentity: identity}
		queryData, _ := json.Marshal(query)
		membershipRequest := work.NewContent("membership-request", "project-scope", "application/json", queryData, s.now())
		membershipResponse := work.NewContent("membership-response", "project-scope", "application/json", []byte(`{"is_in_chat":true}`), s.now())
		for _, content := range []work.Content{membershipRequest, membershipResponse} {
			if err = tx.PutContent(content); err != nil {
				return err
			}
		}
		membershipOp := work.Operation{ID: "membership-verified", ScopeID: "project-scope", Actor: work.Actor{Kind: work.ActorSystem, ID: "membership-verifier"}, Kind: "feishu.membership.query", TargetID: "group", TargetGeneration: 1, IdempotencyKey: "membership-verified", RequestRef: membershipRequest.ID, RequestHash: membershipRequest.SHA256, ExternalReceipt: membershipResponse.ID, State: work.OperationConfirmed, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, err = tx.PutOperation(membershipOp, 0); err != nil {
			return err
		}
		if err = tx.RecordMembershipCheck(work.MembershipCheck{ID: "membership-check", ScopeID: "project-scope", PrincipalID: interaction.PrincipalID, Gateway: interaction.Gateway, Tenant: "tenant", ChatID: "group", BindingGeneration: 1, QueriedIdentity: identity, Status: work.MembershipPresent, OperationID: membershipOp.ID, ResponseRef: membershipResponse.ID, ObservedAt: s.now(), ExpiresAt: s.now().Add(time.Hour)}); err != nil {
			return err
		}
		prompt := work.NewContent("business-question", "project-scope", "text/plain", []byte("是否使用这版方案？"), s.now())
		if err = tx.PutContent(prompt); err != nil {
			return err
		}
		actor := work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}
		request = work.DecisionRequest{ID: "human-business", ScopeID: "project-scope", Kind: work.DecisionBusiness, State: work.DecisionOpen, Owner: actor, AllowedActors: []work.Actor{actor}, Presentation: &work.DecisionPresentation{Title: "确认方案", PromptRef: prompt.ID, Choices: []work.DecisionChoice{{Value: "approve", Label: "通过"}, {Value: "revise", Label: "请修改"}}}, RequestGeneration: 1, Revision: 1, CreatedAt: s.now()}
		if err = tx.PutDecisionRequest(request, 0); err != nil {
			return err
		}
		projection, _ := json.Marshal(map[string]string{"card": "actual frozen prompt"})
		body := work.NewContent("business-card-body", request.ScopeID, "application/json", projection, s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		op := work.Operation{ID: "business-card-publish", ScopeID: request.ScopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "ui"}, Kind: "feishu.publish", TargetID: "group", TargetGeneration: 1, IdempotencyKey: "card-publish", RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		return tx.PutUIView(workstore.UIView{ID: viewID, ScopeID: request.ScopeID, SurfaceKey: "business.decision:" + request.ID, Kind: "business.decision", TargetID: request.ID, TargetRevision: 1, Binding: binding, Tenant: "tenant", ChatID: "group", PrincipalID: interaction.PrincipalID, ChannelGeneration: 1, BodyRef: body.ID, OperationID: op.ID, ExternalMessageID: "actual-card-message", Revision: 1, CreatedAt: s.now()}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DecisionRespond(t.Context(), token, run, DecisionRespondRequest{Key: "model-fake-human", RequestID: request.ID, ExpectedRevision: 1, RequestGeneration: 1, Choice: "approve"}); err == nil {
		t.Fatal("model impersonated human owner")
	}
	if _, err = s.RespondHumanDecision(t.Context(), interaction.ID, viewID, "revise"); err == nil {
		t.Fatal("server replaced actual user's offered choice")
	}
	result, err := s.RespondHumanDecision(t.Context(), interaction.ID, viewID, "approve")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.RespondHumanDecision(t.Context(), interaction.ID, viewID, "approve")
	if err != nil || !replay.Replay || replay.OperationID != result.OperationID {
		t.Fatalf("durable response replay %+v %v", replay, err)
	}
}
