package workservice

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

// Fixture follows the real durable publication receipt and SDK reply records;
// the production path must not infer a version from the currently newest task.
func installReviewReply(t *testing.T, s *Service, token, run, taskID, submissionID string) {
	t.Helper()
	err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		content := work.NewContent("review-artifact", "scope", "text/plain", []byte("frozen review artifact"), s.now())
		if e := tx.PutContent(content); e != nil {
			return e
		}
		if e := tx.PutResource(work.Resource{ID: "review-resource", ScopeID: "scope", ConnectorIdentity: "bound", SourceIdentity: "docx:review", CanonicalURI: "https://a.feishu.cn/docx/review", Purpose: "Review", CollectionID: "artifacts", Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutResourceVersion(work.ResourceVersion{ID: "review-version", ResourceID: "review-resource", ScopeID: "scope", Kind: work.VersionImmutableSnapshot, Source: work.SourceRef{Kind: "docx", ID: "review", Version: "1", EvidenceRef: content.ID}, ContentRef: content.ID, ContentHash: content.SHA256, Consistency: work.ConsistencyAtomic, Complete: true, Saved: true, StartedAt: s.now(), CapturedAt: s.now()})
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := s.RequestDecision(t.Context(), token, run, RequestDecisionRequest{Key: "actual-review", Title: "请审阅这一版", Prompt: "确认指定要求与保存成果", TaskID: taskID, SourceMessageID: "human-input", SubmissionID: submissionID, References: []work.DeliveryRef{{ResourceID: "review-resource", VersionID: "review-version"}}, Choices: []work.DecisionChoice{{Value: "approve", Label: "认可这一版"}, {Value: "changes", Label: "需要修改"}}})
	if err != nil {
		t.Fatal(err)
	}
	err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		binding, e := tx.GetRoleBotBinding("leader")
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(MessageProjection{UIViewID: "actual-review-view", RoleID: "leader", GatewayRef: "gateway", AppID: "app", BotGeneration: 1, Tenant: "tenant", ChatID: "personal-chat", TaskID: taskID, Card: json.RawMessage(`{"schema":"2.0"}`)})
		body := work.NewContent("actual-review-publication", "scope", "application/json", raw, s.now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: "actual-review-send", ScopeID: "scope", ContextID: "sharing-context", Actor: work.Actor{Kind: work.ActorRole, ID: "leader"}, Kind: "feishu.publish", TargetID: "personal-chat", TargetGeneration: 1, IdempotencyKey: "actual-review-send", RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationPrepared, Revision: 1}
		if _, _, e = tx.PutOperation(op, 0); e != nil {
			return e
		}
		op.State = work.OperationSending
		op.Revision++
		if _, _, e = tx.PutOperation(op, 1); e != nil {
			return e
		}
		op.State = work.OperationConfirmed
		op.Revision++
		op.ExternalReceipt = `{"message_id":"review-card","chat_id":"personal-chat"}`
		if _, _, e = tx.PutOperation(op, 2); e != nil {
			return e
		}
		out := work.Outbox{ID: "actual-review-out", ScopeID: "scope", OperationID: op.ID, Target: "personal-chat", EventID: "actual-review", PayloadRef: body.ID, State: work.OutboxPending, Revision: 1}
		if e = tx.PutOutbox(out, 0); e != nil {
			return e
		}
		out.State = work.OutboxSending
		out.Revision++
		if e = tx.PutOutbox(out, 1); e != nil {
			return e
		}
		out.State = work.OutboxSent
		out.ExternalMessageID = "review-card"
		out.Revision++
		if e = tx.PutOutbox(out, 2); e != nil {
			return e
		}
		view := workstore.UIView{ID: "actual-review-view", ScopeID: "scope", ContextID: "sharing-context", SurfaceKey: "business.decision:" + decision.ObjectID, Kind: "business.decision", TargetID: decision.ObjectID, TargetRevision: 1, Binding: binding, Tenant: "tenant", ChatID: "personal-chat", PrincipalID: "human", BodyRef: body.ID, OperationID: op.ID, ExternalMessageID: "review-card", Revision: 1}
		if e = tx.PutUIView(view, 0); e != nil {
			return e
		}
		if e = tx.PutPlatformMessageMapping(work.PlatformMessageMapping{Tenant: "tenant", ChatID: "personal-chat", MessageID: "review-card", ScopeID: "scope", TaskID: taskID, RootMessageID: "review-card", OperationID: op.ID, OutboxID: out.ID, RoleID: "leader", BotGeneration: 1}); e != nil {
			return e
		}
		ingress, _, e := tx.AcceptRawIngress(work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "real-review-reply", Tenant: "tenant", ChatID: "personal-chat", ChatType: "p2p", MessageID: "platform-message", ParentID: "review-card", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human-open"}, MessageType: "text", RawContent: `{"text":"这一版可以"}`, ReceivedAt: s.now()}, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "leader", BotGeneration: 1})
		if e != nil {
			return e
		}
		if e = tx.FreezePrivateIngressNavigation(ingress.ID, work.PrivateNavigation{PersonalScopeID: "scope", PrincipalID: "human", RoleID: "leader", Mode: work.PrivatePersonal, FrontContextID: "sharing-context", Revision: 1}); e != nil {
			return e
		}
		ingress, e = tx.GetIngress(ingress.ID)
		if e != nil {
			return e
		}
		old := ingress.Revision
		ingress.State = work.IngressProcessed
		ingress.MessageID = "human-input"
		ingress.Revision++
		return tx.PutIngress(ingress, old)
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestReviewOldHumanReplyCannotApproveAnotherSubmission(t *testing.T) {
	s, token, run := knowledgeService(t)
	task := lifecycleTask(t, s, token, run, true)
	started, err := s.TaskStart(t.Context(), token, run, TaskTransitionRequest{Key: "start", TaskID: task.ObjectID, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	old := submitLifecycleEvidence(t, s, token, run, task.ObjectID, "V1", started.Revision)
	newer := submitLifecycleEvidence(t, s, token, run, task.ObjectID, "V2", started.Revision)
	installReviewReply(t, s, token, run, task.ObjectID, old.ObjectID)
	if _, err = s.TaskReview(t.Context(), token, run, TaskEvidenceAdoptRequest{Key: "borrow-old-approval", TaskID: task.ObjectID, ExpectedRevision: started.Revision, SubmissionID: newer.ObjectID, SourceMessageID: "human-input"}); err == nil {
		t.Fatal("reply to frozen V1 review approved undisplayed V2")
	}
	if _, err = s.TaskDone(t.Context(), token, run, TaskTransitionRequest{Key: "not-approved", TaskID: task.ObjectID, ExpectedRevision: started.Revision}); err == nil {
		t.Fatal("rejected cross-version approval became completion evidence")
	}
	if _, err = s.TaskReview(t.Context(), token, run, TaskEvidenceAdoptRequest{Key: "exact-old-review", TaskID: task.ObjectID, ExpectedRevision: started.Revision, SubmissionID: old.ObjectID, SourceMessageID: "human-input"}); err != nil {
		t.Fatalf("actual reply to exact still-applicable submission rejected: %v", err)
	}
	if err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		requests, e := tx.ListDecisionRequests("scope")
		if e != nil {
			return e
		}
		if len(requests) != 1 || requests[0].State != work.DecisionResolved {
			t.Fatal("natural exact approval left the human decision pending")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
