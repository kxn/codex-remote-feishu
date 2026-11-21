package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workprovision"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type projectNavigationPlatform struct {
	workprovision.Platform
	binding work.RoleBotBinding
	pin     string
	calls   int
}

func (p *projectNavigationPlatform) Binding() work.RoleBotBinding { return p.binding }
func (p *projectNavigationPlatform) HasNavigationPin(_ context.Context, _, id string) (bool, error) {
	return p.pin == id, nil
}
func (p *projectNavigationPlatform) PinNavigation(_ context.Context, id string) (string, error) {
	p.calls++
	p.pin = id
	return id, nil
}

func TestProjectNavigationTickPublishesBeforePinAndDetectsHumanRemoval(t *testing.T) {
	ctx := t.Context()
	fixture := newResourceRecoveryFixture(t)
	app := fixture.a
	r := app.teamRuntime
	platform := &projectNavigationPlatform{}
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		platform.binding, err = tx.GetRoleBotBinding("leader")
		if err != nil {
			return err
		}
		const opID = "provision-project"
		actor := work.Actor{Kind: work.ActorPrincipal, ID: "owner"}
		raw, _ := json.Marshal(workservice.CreateProjectRequest{Tenant: "tenant", ChatID: "chat", PrincipalID: actor.ID})
		body := work.NewContent(opID+":request", "scope", "application/json", raw, time.Now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		if _, _, err = tx.PutOperation(work.Operation{ID: opID, ScopeID: "scope", Actor: actor, Kind: "project.provision", TargetID: "project", TargetGeneration: 1, IdempotencyKey: opID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationConfirmed, ExternalReceipt: "provisioned", Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutProjectHome(workstore.ProjectHome{ID: "project", ScopeID: "scope", OperationID: opID, ChannelGeneration: 1, Identity: platform.binding, NavigationCardWanted: true, SyncState: "pending", Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	var err error
	r.provision, err = workprovision.New(r.store, workprovision.Options{
		Resolver: workprovision.ResolverFunc(func(context.Context, work.RoleBotBinding) (workprovision.Platform, error) { return platform, nil }),
		Execution: workprovision.ExecutionPreparerFunc(func(context.Context, work.Project, []string) error {
			t.Fatal("navigation started execution")
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	r.publisher, err = workpublish.New(r.store, recoveryUISender{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	tick := func() {
		t.Helper()
		if err := app.runTeamProjectNavigation(ctx); err != nil {
			t.Fatal(err)
		}
	}
	tick()
	tick()
	if platform.calls != 0 {
		t.Fatal("unconfirmed card was pinned")
	}
	if _, err = r.publisher.RunOnce(ctx, 32); err != nil {
		t.Fatal(err)
	}
	tick()
	tick()
	if platform.calls != 1 || platform.pin == "" {
		t.Fatalf("confirmed publication did not pin exactly once: %+v", platform)
	}
	platform.pin = ""
	tick()
	tick()
	if platform.calls != 1 {
		t.Fatal("navigation tick fought a human unpin")
	}
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		home, err := tx.GetProjectHome("project")
		if err != nil {
			return err
		}
		view, err := tx.GetUIView(home.NavigationCardViewID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent("scope", view.BodyRef)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body.Data), "project.navigation.open") || !strings.Contains(string(body.Data), "project.navigation.pause") {
			t.Fatalf("navigation lacks work and pause actions: %s", body.Data)
		}
		if !home.NavigationPinMissing || home.NavigationGeneration != 1 {
			t.Fatalf("unpin not persisted: %+v", home)
		}
		facts, err := teamProjectFacts(tx, "project")
		if err != nil {
			return err
		}
		panel := teamProjectPanel(facts)
		for _, button := range panel.Buttons {
			if button.Action == "project.navigation.restore" {
				return nil
			}
		}
		t.Fatal("unpin had no restore action")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Run("reject-view-invalidated-after-original-check", func(t *testing.T) {
		var original workstore.UIView
		var interaction workstore.HumanInteraction
		if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
			home, err := tx.GetProjectHome("project")
			if err != nil {
				return err
			}
			original, err = tx.GetUIView(home.NavigationCardViewID)
			if err != nil {
				return err
			}
			gateway := work.IngressGateway{GatewayRef: original.Binding.GatewayRef, AppID: original.Binding.AppID, RoleID: original.Binding.RoleID, BotGeneration: original.Binding.IdentityGeneration}
			ids := work.SenderIDs{OpenID: "owner"}
			record, _, err := tx.AcceptRawIngress(work.RawIngressEnvelope{
				GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "navigation-owner-source",
				Tenant: original.Tenant, SenderTenant: original.Tenant, SenderIDs: ids, ChatID: original.ChatID,
				ChatType: "group", MessageID: "navigation-membership", MessageType: "text", SenderType: "user",
				RawContent: `{"text":"Verify this project member"}`, ReceivedAt: fixture.now, MessageCreatedAt: fixture.now,
			}, gateway)
			if err != nil {
				return err
			}
			if err = tx.GrantProjectAccessFromIngress(original.ScopeID, fixture.owner, record.ID, fixture.now.Add(time.Hour)); err != nil {
				return err
			}
			interaction, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{
				GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "navigation-pause",
				Tenant: original.Tenant, SenderIDs: ids, ChatID: original.ChatID, MessageID: original.ExternalMessageID,
				Kind: "card", Values: map[string]any{"team_view": original.ID, "action": "project.navigation.pause"}, ReceivedAt: fixture.now,
			}, gateway)
			if err != nil {
				return err
			}
			// The check before the slow membership call accepts the old view.
			return tx.ValidateViewInteraction(original, interaction)
		}); err != nil {
			t.Fatal(err)
		}
		if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
			changed := original
			changed.Revision++
			changed.Invalidated = true
			return tx.PutUIView(changed, original.Revision)
		}); err != nil {
			t.Fatal(err)
		}
		if err := r.service.PauseFromProjectNavigation(ctx, interaction.ID, original.ID); err == nil {
			t.Fatal("stale navigation action passed the mutation transaction")
		}
		if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
			project, err := tx.GetProject("project")
			if err != nil {
				return err
			}
			if project.State != work.ProjectActive || project.Revision != 1 {
				t.Fatalf("stale action mutated the project: %+v", project)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}
