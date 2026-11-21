package workservice

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type resourceFake struct {
	resolve func(context.Context, string) (ResolvedResource, error)
	capture func(context.Context, work.Resource, CaptureResourceRequest) (CapturedResource, error)
}

func (f *resourceFake) Identity() string { return "bound-connector" }
func (f *resourceFake) Resolve(ctx context.Context, url string) (ResolvedResource, error) {
	return f.resolve(ctx, url)
}
func (f *resourceFake) Capture(ctx context.Context, r work.Resource, req CaptureResourceRequest) (CapturedResource, error) {
	return f.capture(ctx, r, req)
}

func TestResourceResolveCanonicalDedupCaptureAndPendingDelivery(t *testing.T) {
	s, token, run := knowledgeService(t)
	ctx := context.Background()
	resolveCalls, captureCalls := 0, 0
	connector := &resourceFake{}
	connector.resolve = func(ctx context.Context, _ string) (ResolvedResource, error) {
		resolveCalls++
		// One connection is configured. This fails by deadline if the service held
		// its SQL transaction across the connector call.
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := s.store.Within(bounded, func(tx *workstore.Tx) error { _, err := tx.GetScope("scope"); return err }); err != nil {
			return ResolvedResource{}, err
		}
		return ResolvedResource{SourceIdentity: "docx:canonical-document", CanonicalURI: "https://a.feishu.cn/docx/canonical-document", Source: work.SourceRef{Kind: "feishu_docx", ID: "canonical-document", Version: "7", EvidenceRef: "actual-request"}}, nil
	}
	connector.capture = func(context.Context, work.Resource, CaptureResourceRequest) (CapturedResource, error) {
		captureCalls++
		return CapturedResource{Kind: work.VersionImmutableSnapshot, Source: work.SourceRef{Kind: "feishu_docx", ID: "canonical-document", Version: "7", EvidenceRef: "actual-request"}, Content: []byte(`{"revision":7,"blocks":["saved"]}`), MediaType: "application/json", ObservationRange: "document revision 7", Consistency: work.ConsistencyAtomic, Complete: true, StartedAt: s.now(), CapturedAt: s.now()}, nil
	}
	s.connectors = map[string]ResourceConnector{"leader": connector}
	req := RegisterResourceRequest{Key: "register", CollectionID: "materials", SourceURL: "https://a.feishu.cn/wiki/navigation", Purpose: "Project home"}
	first, err := s.ResourceRegister(ctx, token, run, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.ResourceRegister(ctx, token, run, req)
	if err != nil || !replay.Replay || replay.ObjectID != first.ObjectID || resolveCalls != 1 {
		t.Fatalf("register replay %#v calls=%d err=%v", replay, resolveCalls, err)
	}
	req.Key = "alias"
	req.SourceURL = "https://a.feishu.cn/docx/canonical-document"
	req.CollectionID = "deliverables"
	alias, err := s.ResourceRegister(ctx, token, run, req)
	if err != nil || alias.ObjectID != first.ObjectID {
		t.Fatalf("canonical identity not deduped %#v, %v", alias, err)
	}
	captured, err := s.ResourceCapture(ctx, token, run, CaptureResourceRequest{Key: "capture", ResourceID: first.ObjectID})
	if err != nil || captureCalls != 1 {
		t.Fatalf("capture %#v, %v", captured, err)
	}
	view, err := s.ResourceReadVersion(ctx, token, run, ReadResourceVersionRequest{ResourceID: first.ObjectID, VersionID: captured.ObjectID, Limit: 10})
	if err != nil || view.Content == nil || view.Content.NextOffset == nil || view.Version.Source.Version != "7" {
		t.Fatalf("saved snapshot %#v, %v", view, err)
	}
	delivery, err := s.PublishDelivery(ctx, token, run, PublishDeliveryRequest{Key: "deliver", Purpose: "Project result", Summary: "Saved revision seven.", References: []work.DeliveryRef{{ResourceID: first.ObjectID, VersionID: captured.ObjectID}}, Audience: []work.Actor{{Kind: work.ActorPrincipal, ID: "human"}}, Retention: work.RetentionPermanent, ReplyToInputID: "human-inbox"})
	if err != nil || delivery.Status != "accepted" {
		t.Fatalf("delivery %#v, %v", delivery, err)
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		stored, err := tx.GetDelivery("scope", delivery.ObjectID)
		if err != nil {
			return err
		}
		outbox, err := tx.GetOutbox(stored.NotificationOutboxID)
		if err != nil {
			return err
		}
		if outbox.State != work.OutboxPending || outbox.ExternalMessageID != "" {
			t.Error("publishing fabricated a platform send receipt")
		}
		operation, err := tx.GetOperation(outbox.OperationID)
		if err != nil {
			return err
		}
		if operation.Kind != "feishu.publish" || operation.RequestRef != outbox.PayloadRef {
			t.Error("notification cannot use the shared platform sender")
		}
		body, err := tx.GetContent("scope", outbox.PayloadRef)
		if err != nil {
			return err
		}
		var projection MessageProjection
		if err = json.Unmarshal(body.Data, &projection); err != nil {
			return err
		}
		if projection.ChatID != "personal-chat" || projection.BotGeneration != 1 || projection.Body == "" {
			t.Error("delivery lost actual conversation/bot binding")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownResourceOperationNeedsExplicitReconciliationAndFrozenIdentity(t *testing.T) {
	s, token, run := knowledgeService(t)
	calls := 0
	connector := &resourceFake{resolve: func(context.Context, string) (ResolvedResource, error) {
		calls++
		if calls == 1 {
			return ResolvedResource{}, context.DeadlineExceeded
		}
		return ResolvedResource{SourceIdentity: "docx:known", CanonicalURI: "https://a.feishu.cn/docx/known", Source: work.SourceRef{Kind: "docx", ID: "known", Version: "1", EvidenceRef: "request"}}, nil
	}}
	s.connectors = map[string]ResourceConnector{"leader": connector}
	req := RegisterResourceRequest{Key: "uncertain", CollectionID: "materials", SourceURL: "https://a.feishu.cn/docx/known", Purpose: "Known"}
	pending, err := s.ResourceRegister(context.Background(), token, run, req)
	if err != nil || pending.Status != "pending_external" {
		t.Fatalf("unknown %#v, %v", pending, err)
	}
	if _, err := s.ResourceRegister(context.Background(), token, run, req); err != nil || calls != 1 {
		t.Fatalf("unknown was silently replayed calls=%d err=%v", calls, err)
	}
	complete, err := s.ResourceReconcile(context.Background(), token, run, ReconcileResourceRequest{OperationID: pending.OperationID})
	if err != nil || complete.Status != "completed" || calls != 2 {
		t.Fatalf("reconcile %#v calls=%d err=%v", complete, calls, err)
	}
	changed := req
	changed.SourceURL = "https://a.feishu.cn/docx/other"
	if _, err := s.ResourceRegister(context.Background(), token, run, changed); !errors.Is(err, workstore.ErrIdempotencyConflict) {
		t.Fatalf("same key changed source: %v", err)
	}
}

func TestResourceResolverReceivesFreshBoundIdentity(t *testing.T) {
	s, token, run := knowledgeService(t)
	var got []work.RoleBotBinding
	connector := &resourceFake{resolve: func(context.Context, string) (ResolvedResource, error) {
		return ResolvedResource{SourceIdentity: "docx:fresh", CanonicalURI: "https://a.feishu.cn/docx/fresh", Source: work.SourceRef{Kind: "feishu_docx", ID: "fresh", Version: "1", EvidenceRef: "actual-resolve"}}, nil
	}}
	dynamic, err := NewWithResourceConnectorResolver(s.store, s.signer, s.now, ResourceConnectorResolverFunc(func(_ context.Context, b work.RoleBotBinding) (ResourceConnector, error) {
		got = append(got, b)
		return connector, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = dynamic.ResourceRegister(context.Background(), token, run, RegisterResourceRequest{Key: "fresh", CollectionID: "materials", SourceURL: "https://a.feishu.cn/docx/fresh", Purpose: "Current binding"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("dynamic resolver was not used")
	}
	if err = s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		b, err := tx.GetRoleBotBinding("leader")
		if err != nil {
			return err
		}
		for _, observed := range got {
			if observed != b {
				t.Fatalf("resolver received stale/unrelated credential: %#v want %#v", observed, b)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
