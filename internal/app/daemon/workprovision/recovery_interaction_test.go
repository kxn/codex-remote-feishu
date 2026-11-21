package workprovision

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (p *fakePlatform) ResolveRecoveryDocument(ctx context.Context, link string) (HomeDocument, error) {
	parsed, _ := url.Parse(link)
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	return p.ReadDocument(ctx, parts[len(parts)-1])
}
func TestRecoveryActualHumanFormAdoptsVerifiedDocumentAndPreservesUnknownAudit(t *testing.T) {
	identity := work.IdentityEvidence{Tenant: "tenant", AppID: "app-leader", Generation: 1, IDs: work.SenderIDs{OpenID: "recovery-person"}, ObservationRef: "actual-sdk-source"}
	f := newFixtureForSDK(t, &identity)
	f.leader.createErr = context.DeadlineExceeded
	if report, err := f.service.Run(t.Context(), f.operationID); err != nil || report.State != "unknown" {
		t.Fatalf("initial unknown %+v %v", report, err)
	}
	f.now = f.now.Add(time.Minute)
	var recovery Recovery
	err := f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		step, err := tx.GetProvisionStep(f.operationID, "materials.document")
		if err != nil {
			return err
		}
		binding := f.leader.binding
		gateway := work.IngressGateway{RoleID: binding.RoleID, GatewayRef: binding.GatewayRef, AppID: binding.AppID, BotGeneration: binding.IdentityGeneration}
		raw := work.HumanInteractionEnvelope{GatewayRef: binding.GatewayRef, AppID: binding.AppID, EventID: "actual-recovery-click", Tenant: "tenant", SenderIDs: identity.IDs, ChatID: "project-chat", MessageID: "actual-recovery-card", Kind: "card", Values: map[string]any{"team_view": "recovery-view", "action": "recovery.resolve"}, Form: map[string]any{"external_id": f.leader.doc.URL}, ReceivedAt: f.now}
		interaction, _, err := tx.AcceptHumanInteraction(raw, gateway)
		if err != nil {
			return err
		}
		queried := work.IdentityScope{Tenant: "tenant", AppIdentityDomain: binding.AppID, IDType: "open_id", ExternalID: identity.IDs.OpenID, Generation: 1}
		query := workstore.MembershipQuery{ScopeID: f.scopeID, PrincipalID: interaction.PrincipalID, Tenant: "tenant", ChatID: "project-chat", BindingGeneration: 1, QueriedIdentity: queried}
		encoded, _ := json.Marshal(query)
		queryBody := work.NewContent("recovery-membership-request", f.scopeID, "application/json", encoded, f.now)
		response := work.NewContent("recovery-membership-response", f.scopeID, "application/json", []byte(`{"is_in_chat":true}`), f.now)
		for _, body := range []work.Content{queryBody, response} {
			if err = tx.PutContent(body); err != nil {
				return err
			}
		}
		queryOp := work.Operation{ID: "recovery-membership", ScopeID: f.scopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "membership-verifier"}, Kind: "feishu.membership.query", TargetID: "project-chat", TargetGeneration: 1, IdempotencyKey: "recovery-membership", RequestRef: queryBody.ID, RequestHash: queryBody.SHA256, ExternalReceipt: response.ID, State: work.OperationConfirmed, Revision: 1, CreatedAt: f.now, UpdatedAt: f.now}
		if _, _, err = tx.PutOperation(queryOp, 0); err != nil {
			return err
		}
		if err = tx.RecordMembershipCheck(work.MembershipCheck{ID: "recovery-membership-check", ScopeID: f.scopeID, PrincipalID: interaction.PrincipalID, Gateway: gateway, Tenant: "tenant", ChatID: "project-chat", BindingGeneration: 1, QueriedIdentity: queried, Status: work.MembershipPresent, OperationID: queryOp.ID, ResponseRef: response.ID, ObservedAt: f.now, ExpiresAt: f.now.Add(time.Hour)}); err != nil {
			return err
		}
		frozen := RecoveryInteractionTarget{OperationID: f.operationID, StepID: step.StepID, ExpectedRevision: step.Revision}
		target, _ := json.Marshal(frozen)
		if err = tx.PutContent(work.NewContent("recovery-view:recovery-target", f.scopeID, "application/json", target, f.now)); err != nil {
			return err
		}
		card := work.NewContent("recovery-card-body", f.scopeID, "application/json", []byte(`{}`), f.now)
		if err = tx.PutContent(card); err != nil {
			return err
		}
		op := work.Operation{ID: "recovery-view-publish", ScopeID: f.scopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "ui"}, Kind: "feishu.publish", TargetID: "project-chat", TargetGeneration: 1, IdempotencyKey: "recovery-view-publish", RequestRef: card.ID, RequestHash: card.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: f.now, UpdatedAt: f.now}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		if err = tx.PutUIView(workstore.UIView{ID: "recovery-view", ScopeID: f.scopeID, SurfaceKey: "recovery-document", Kind: "project.recovery", TargetID: f.operationID, TargetRevision: step.Revision, Binding: binding, Tenant: "tenant", ChatID: "project-chat", PrincipalID: interaction.PrincipalID, ChannelGeneration: 1, BodyRef: card.ID, OperationID: op.ID, ExternalMessageID: "actual-recovery-card", Revision: 1, CreatedAt: f.now}, 0); err != nil {
			return err
		}
		recovery = Recovery{OperationID: f.operationID, StepID: step.StepID, ExpectedRevision: step.Revision, InteractionID: interaction.ID, ViewID: "recovery-view", CandidateURL: f.leader.doc.URL}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wrong := recovery
	wrong.CandidateURL = "https://a.feishu.cn/docx/different"
	if _, err = f.service.ResolveUnknown(t.Context(), wrong); err == nil {
		t.Fatal("caller substituted user's actual selected resource")
	}
	report, err := f.service.ResolveUnknown(t.Context(), recovery)
	if err != nil || report.State != "confirmed" {
		t.Fatalf("human recovery %+v %v", report, err)
	}
	if f.leader.documentCalls != 1 {
		t.Fatal("unknown document was blindly recreated")
	}
	if _, err = f.service.ResolveUnknown(t.Context(), recovery); err != nil {
		t.Fatalf("actual same click lost replay: %v", err)
	}
	if err = f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		steps, err := tx.ListProvisionSteps(f.operationID)
		if err != nil {
			return err
		}
		found := false
		for _, step := range steps {
			if strings.HasPrefix(step.StepID, "recovery.materials.document.") {
				found = strings.Contains(string(step.Receipt), `"original_call_state":"unknown"`) && strings.Contains(string(step.Receipt), "human_selected_verified_existing")
			}
		}
		if !found {
			t.Fatal("unknown original creation was silently rewritten as known success")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
