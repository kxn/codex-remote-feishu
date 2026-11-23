package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

type noTeamRoute struct{}

func (noTeamRoute) Resolve(context.Context, ingressowner.Event, *ingressowner.PlatformMessage) (ingressowner.Owner, error) {
	return ingressowner.Owner{}, errors.New("unexpected team owner")
}

type noTeamAdmission struct{}

func (noTeamAdmission) Admit(context.Context, ingressowner.Record) (ingressowner.AdmissionReceipt, error) {
	return ingressowner.AdmissionReceipt{}, errors.New("unexpected team admission")
}
func TestPrivateSDKMessageRoutesAndReplaysThroughApplicationJournal(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	router, err := ingressowner.Open(filepath.Join(root, "router.db"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	j, err := privatechat.OpenJournal(filepath.Join(root, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	host := New(":0", ":0", &privatePublishGateway{}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	processor, err := privatechat.NewProcessor(app, j, privateActionDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	wakes := 0
	private := privateApplicationAdmitter{host: host, router: router, journal: j, processor: processor, wake: func() { wakes++ }, refresh: func(context.Context, *privatechat.Processor, privatechat.Owner, int64) error { return nil }}
	entry := &applicationIngress{host: host, router: router, resolver: applicationOwnerResolver{router: router}, private: private, team: noTeamAdmission{}}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "sdk-event", Tenant: "tenant", SenderTenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "message", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
	event, err := ingressmapper.Message(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	record, err := entry.deliver(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != ingressowner.Accepted || record.Owner == nil || record.Owner.Application != ingressowner.Private || record.Receipt == nil || wakes != 1 {
		t.Fatalf("private route: %+v wakes=%d", record, wakes)
	}
	groupRaw := raw
	groupRaw.EventID = "group-event"
	groupRaw.ChatType = "group"
	groupRaw.ChatID = "group"
	groupRaw.MessageID = "group-message"
	groupEvent, e := ingressmapper.Message(groupRaw, 1)
	if e != nil {
		t.Fatal(e)
	}
	group, _, e := router.Admit(ctx, groupEvent, entry.resolver)
	if e != nil || group.Owner == nil || group.Owner.Application != ingressowner.Team || *group.Owner == *record.Owner {
		t.Fatalf("group ownership: %+v %v", group, e)
	}
	record2, err := entry.deliver(ctx, event)
	if err != nil || record2.Sequence != record.Sequence || wakes != 1 {
		t.Fatalf("replay: %+v %v", record2, err)
	}
}

type testPrivateStager struct{ calls int }

func (s *testPrivateStager) StagePrivateMessage(_ context.Context, raw platformingress.RawIngressEnvelope, _ string) (control.Action, bool, error) {
	s.calls++
	return control.Action{Kind: control.ActionImageMessage, GatewayID: raw.GatewayRef, SurfaceSessionID: gatewaypkg.SurfaceIDForInbound(raw.GatewayRef, "", "p2p", raw.SenderIDs.OpenID), ChatID: raw.ChatID, ActorUserID: raw.SenderIDs.OpenID, MessageID: raw.MessageID, LocalPath: "/tmp/staged-image", MIMEType: "image/png"}, true, nil
}
func TestPrivateMediaIsDurablyDeferredThenStagedOnce(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	router, err := ingressowner.Open(filepath.Join(root, "router.db"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	j, err := privatechat.OpenJournal(filepath.Join(root, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	host := New(":0", ":0", &privatePublishGateway{}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	processor, err := privatechat.NewProcessor(app, j, privateActionDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	stager := &testPrivateStager{}
	private := privateApplicationAdmitter{host: host, router: router, journal: j, processor: processor, wake: func() {}, refresh: func(context.Context, *privatechat.Processor, privatechat.Owner, int64) error { return nil }}
	entry := &applicationIngress{host: host, router: router, resolver: applicationOwnerResolver{router: router}, private: private, team: noTeamAdmission{}, stageWake: make(chan struct{}, 1), stager: stager}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "image-event", Tenant: "tenant", SenderTenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "image-message", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "image", RawContent: `{"image_key":"image-key"}`, ReceivedAt: time.Now()}
	event, err := ingressmapper.Message(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.deliver(ctx, event); err != nil {
		t.Fatal(err)
	}
	record, err := router.Get(ctx, event.Key)
	if err != nil || record.State != ingressowner.Pending || stager.calls != 0 {
		t.Fatalf("not durably deferred: %+v %v", record, err)
	}
	if err = entry.stagePrivate(ctx, record); err != nil {
		t.Fatal(err)
	}
	record, err = router.Get(ctx, event.Key)
	if err != nil || record.State != ingressowner.Accepted || stager.calls != 1 {
		t.Fatalf("not admitted: %+v %v", record, err)
	}
	pending, err := j.PendingIngressWork(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("private input missing: %+v %v", pending, err)
	}
	if _, err = entry.deliver(ctx, event); err != nil || stager.calls != 1 {
		t.Fatal("replay repeated media staging")
	}
}
