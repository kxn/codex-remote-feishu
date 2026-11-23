package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateProjectSDKJourney(t *testing.T) {
	ctx := t.Context()
	source, store, _ := shutdownFixtureWithWitness(t, false, true)
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "app", AppSecret: "fixture"}}
	host, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	host.teamRuntime = source.teamRuntime
	host.teamRuntime.app = host
	host.gateway = &privatePublishGateway{}
	role := teamID("role", "gateway")
	now := time.Now().UTC()
	err := store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: role, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: role, Active: true, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "project-scope", Kind: work.ScopeProject, TeamID: "team", ProjectID: "project", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProject(work.Project{ID: "project", ScopeID: "project-scope", TeamID: "team", Name: "Authorized project", LeaderRoleID: role, State: work.ProjectActive, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: role, State: work.ProjectMemberReady, BotGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "group", Purpose: "main", BindingGeneration: 1, Active: true}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "other-scope", Kind: work.ScopeProject, TeamID: "team", ProjectID: "other-project", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProject(work.Project{ID: "other-project", ScopeID: "other-scope", TeamID: "team", Name: "Not authorized project", LeaderRoleID: role, State: work.ProjectActive, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProjectMember(work.ProjectMember{ProjectID: "other-project", RoleID: role, State: work.ProjectMemberReady, BotGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		g := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: role, BotGeneration: 1}
		raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "group", Tenant: "tenant", ChatID: "group", ChatType: "group", MessageID: "group-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: "{\"text\":\"hello\"}", ReceivedAt: now}
		record, _, e := tx.AcceptRawIngress(raw, g)
		if e != nil {
			return e
		}
		p, _, e := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: record.ID})
		if e != nil {
			return e
		}
		return tx.GrantProjectAccessFromIngress("project-scope", p.ID, record.ID, now.Add(time.Hour))
	})
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(t.TempDir(), "private.db")
	j, err := privatechat.OpenJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	owner := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", ChatID: "chat", UserID: "human", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "human")}
	binding := ingressowner.PrivateBinding{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BindingGeneration: 1, Operator: ingressowner.Operator{IDType: "open_id", ID: "human"}, ChatID: "chat", Owner: ingressowner.Owner{Application: ingressowner.Private, ID: "opaque", Epoch: 1}}
	adapter := &privatePublicationBinding{journal: j, bindingFor: func(context.Context, privatechat.Owner, int64) (ingressowner.PrivateBinding, error) {
		return binding, nil
	}}
	routerPath := filepath.Join(t.TempDir(), "router.db")
	router, err := ingressowner.Open(routerPath, adapter, publicationBindingVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	adapter.router = router
	if err = router.RecordPrivateBinding(ctx, binding, []byte("verified-p2p")); err != nil {
		t.Fatal(err)
	}
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	processor, err := privatechat.NewProcessor(app, j, privateActionDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	host.applications = &applicationRuntime{router: router, journal: j, processor: processor}
	ingress := &applicationIngress{host: host, router: router, resolver: applicationOwnerResolver{router: router}, private: privateApplicationAdmitter{host: host, router: router, journal: j, processor: processor, wake: func() {}, refresh: func(context.Context, *privatechat.Processor, privatechat.Owner, int64) error { return nil }}, team: noTeamAdmission{}}
	send := func(event, message, text string) []privatechat.EffectRecord {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"text": text})
		raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: event, Tenant: "tenant", SenderTenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: message, SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: string(b), ReceivedAt: time.Now()}
		if e := ingress.hook().Receive(ctx, raw); e != nil {
			t.Fatal(e)
		}
		inputs, e := j.PendingIngressWork(ctx, 10)
		if e != nil || len(inputs) != 1 {
			t.Fatalf("inputs: %+v %v", inputs, e)
		}
		effects, e := processor.ApplyIngress(ctx, owner, 1, inputs[0].Sequence)
		if e != nil {
			t.Fatal(e)
		}
		return effects
	}
	effects := send("private-event", "private-message", "/project")
	if len(effects) != 1 {
		t.Fatalf("project effects: %+v", effects)
	}
	pages, err := runPrivateProjectEffect(ctx, host, processor, j, owner, 1, effects[0].Effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 || !strings.Contains(string(pages[0].Effect.Body), "Authorized project") {
		t.Fatalf("authorized project page: %+v", pages)
	}
	if strings.Contains(string(pages[0].Effect.Body), "Not authorized project") {
		t.Fatal("disclosed another project")
	}
	effects = send("draft-event", "draft-message", "/project share project Only this sentence")
	if len(effects) != 1 {
		t.Fatalf("draft effects: %+v", effects)
	}
	pages, err = runPrivateProjectEffect(ctx, host, processor, j, owner, 1, effects[0].Effect.ID)
	if err != nil || len(pages) == 0 {
		t.Fatalf("draft preview: %+v %v", pages, err)
	}
	publisher := &privatePublisher{host: host, processor: processor, journal: j, ownership: adapter}
	if _, err = publisher.Publish(ctx, owner, 1, pages[0].Effect.ID); err != nil {
		t.Fatal(err)
	}
	publication, err := j.LoadPublication(ctx, owner, 1, pages[0].Effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err = json.Unmarshal(publication.Operations[0].Body, &document); err != nil {
		t.Fatal(err)
	}
	var callback map[string]any
	var find func(any)
	find = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if arg, ok := x["action_arg"].(string); ok && strings.HasPrefix(arg, "confirm ") && x["private_view"] != nil {
				callback = x
			}
			for _, child := range x {
				find(child)
			}
		case []any:
			for _, child := range x {
				find(child)
			}
		}
	}
	find(document)
	if callback == nil {
		t.Fatal("no stamped confirmation callback")
	}
	rawCard := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "confirm-event", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "human"}, ChatID: "chat", MessageID: "platform-1", Kind: "card", Values: callback, ReceivedAt: time.Now()}
	card, err := ingressmapper.Card(rawCard, 1)
	if err != nil {
		t.Fatal(err)
	}
	foreign := rawCard
	foreign.EventID = "foreign-confirm"
	foreign.SenderIDs.OpenID = "other-human"
	foreignCard, err := ingressmapper.Card(foreign, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result, e := ingress.deliver(ctx, foreignCard); e == nil && result.State == ingressowner.Accepted {
		t.Fatal("foreign operator confirmed private share")
	}
	if result, e := ingress.deliver(ctx, card); e != nil || result.State != ingressowner.Accepted {
		t.Fatalf("card admission: %+v %v", result, e)
	}
	work, err := j.PendingHostEffects(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	var shareEffect string
	for _, pending := range work {
		event, _, e := privatechat.DecodeStoredEffect(pending.Effect.Effect.Body)
		if e == nil && event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateProjectShare {
			shareEffect = pending.Effect.Effect.ID
		}
	}
	if shareEffect == "" {
		t.Fatalf("no share effect: %+v", work)
	}
	if _, err = runPrivateProjectEffect(ctx, host, processor, j, owner, 1, shareEffect); err != nil {
		t.Fatal(err)
	}
	if err = router.Close(); err != nil {
		t.Fatal(err)
	}
	router, err = ingressowner.Open(routerPath, adapter, publicationBindingVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	adapter.router = router
	ingress.router = router
	ingress.resolver = applicationOwnerResolver{router: router}
	ingress.private = privateApplicationAdmitter{host: host, router: router, journal: j, processor: processor, wake: func() {}, refresh: func(context.Context, *privatechat.Processor, privatechat.Owner, int64) error { return nil }}
	if _, err = ingress.deliver(ctx, card); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := privatechat.OpenJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	drafts, _, err := reopened.ListProjectShareDrafts(ctx, owner, 1, "", 20)
	if err != nil || len(drafts) != 0 {
		t.Fatalf("confirmed draft after restart: %+v %v", drafts, err)
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		inbox, e := tx.ListInbox("project-scope")
		if e != nil {
			return e
		}
		if len(inbox) != 1 || inbox[0].RecipientRoleID != role {
			t.Fatalf("replay created %d inputs", len(inbox))
		}
		msg, e := tx.GetMessage(inbox[0].MessageID)
		if e != nil {
			return e
		}
		body, e := tx.GetContent("project-scope", msg.BodyRef)
		if e != nil {
			return e
		}
		if string(body.Data) != "Only this sentence" || msg.DerivedFrom == nil || msg.DerivedFrom.Kind != "private_share" {
			t.Fatalf("share body/provenance: %+v %q", msg, body.Data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
