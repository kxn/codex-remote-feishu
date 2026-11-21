package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workprovision"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workteamhome"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamMaterialsIntegrationPlatform struct {
	workteamhome.Platform
	opened string
	grants map[string]bool
}

func (p *teamMaterialsIntegrationPlatform) ResolveHome(_ context.Context, url string) (workprovision.HomeDocument, error) {
	p.opened = url
	return workprovision.HomeDocument{ID: "shared-home", URL: url}, nil
}
func (p *teamMaterialsIntegrationPlatform) ReadDocument(_ context.Context, id string) (workprovision.HomeDocument, error) {
	return workprovision.HomeDocument{ID: id, URL: p.opened}, nil
}
func (p *teamMaterialsIntegrationPlatform) HasGroupView(_ context.Context, id, chat string) (bool, error) {
	return id == "shared-home" && p.grants[chat], nil
}
func (p *teamMaterialsIntegrationPlatform) EnsureGroupView(_ context.Context, id, chat string) error {
	if id != "shared-home" {
		return workstore.ErrScopeDenied
	}
	p.grants[chat] = true
	return nil
}

func TestTeamMaterialsHumanSelectionAndProjectSharing(t *testing.T) {
	f := newResourceRecoveryFixture(t)
	ctx := t.Context()
	r := f.a.teamRuntime
	const link = "https://a.feishu.cn/docx/team-home"
	const secondScope, secondProject, secondChat = "scope-two", "project-two", "chat-two"
	gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "leader", BotGeneration: 1}
	binding := work.RoleBotBinding{GatewayRef: "gateway", AppID: "app", RoleID: "leader", IdentityGeneration: 1}
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		actor := work.Actor{Kind: work.ActorPrincipal, ID: f.owner}
		if err := tx.PutScope(work.WorkScope{ID: "team-bootstrap", Kind: work.ScopeBootstrap, PrincipalID: f.owner, RoleID: "leader", Tenant: "tenant", ChatID: "chat", BootstrapOperationID: "team-bootstrap-request", Revision: 1}, 0); err != nil {
			return fmt.Errorf("bootstrap scope: %w", err)
		}
		if _, _, err := tx.PutOperation(work.Operation{ID: "team-bootstrap-request", ScopeID: "team-bootstrap", Actor: actor, Kind: "project.bootstrap", TargetID: "chat", TargetGeneration: 1, IdempotencyKey: "team-bootstrap-request", RequestHash: "actual-team-test", State: work.OperationPrepared, Revision: 1}, 0); err != nil {
			return err
		}
		body := work.NewContent("owner-setup:body", "team-bootstrap", "text/plain", []byte("Create the team."), f.now)
		if err := tx.PutContent(body); err != nil {
			return fmt.Errorf("owner content: %w", err)
		}
		if _, _, err := tx.AcceptMessage(work.Message{ID: "owner-setup", ScopeID: body.ScopeID, ConversationID: "chat", Source: actor, BodyRef: body.ID, Revision: 1, CreatedAt: f.now}, nil); err != nil {
			return fmt.Errorf("owner message: %w", err)
		}
		if err := tx.PutTeamOwnership(work.TeamOwnership{TeamID: "team", PrincipalID: f.owner, Source: work.SourceRef{Kind: "message", ID: "owner-setup", Version: "1", EvidenceRef: body.ID}, UpdatedBy: actor, Revision: 1}, 0); err != nil {
			return fmt.Errorf("team ownership: %w", err)
		}
		if err := tx.PutProjectHome(workstore.ProjectHome{ID: "project", ScopeID: "scope", OperationID: "project-provision", ChannelGeneration: 1, Identity: binding, SyncState: "pending", Revision: 1}, 0); err != nil {
			return fmt.Errorf("first home: %w", err)
		}
		if err := tx.PutScope(work.WorkScope{ID: secondScope, Kind: work.ScopeProject, ProjectID: secondProject, TeamID: "team", Revision: 1}, 0); err != nil {
			return fmt.Errorf("second scope: %w", err)
		}
		if err := tx.PutProject(work.Project{ID: secondProject, ScopeID: secondScope, TeamID: "team", LeaderRoleID: "leader", Name: "Second project", State: work.ProjectActive, Revision: 1}, 0); err != nil {
			return fmt.Errorf("second project: %w", err)
		}
		return tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: secondProject, Tenant: "tenant", ChatID: secondChat, Active: true, Purpose: "main", BindingGeneration: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	queue := func(id, scope, chat string, shareNeeded bool) {
		t.Helper()
		if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
			team, err := tx.GetTeam("team")
			if err != nil {
				return err
			}
			body, err := teamMaterialsCard(id, team, true, team.Materials == nil, shareNeeded)
			if err != nil {
				return err
			}
			_, err = workui.Queue(tx, workstore.UIView{ID: id, ScopeID: scope, SurfaceKey: id, Kind: "team.materials", TargetID: team.ID, TargetRevision: team.Revision, Binding: binding, Tenant: "tenant", ChatID: chat, ChannelGeneration: 1, PrincipalID: f.owner}, body)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.publisher.RunOnce(ctx, 16); err != nil {
			t.Fatal(err)
		}
	}
	queue("select-team-materials", "scope", "chat", false)
	outsider := f.click(t, "team.materials", "team.materials.select", "other", map[string]any{"materials_mode": "existing", "materials_url": link})
	if _, err := r.service.SelectTeamMaterialsFromInteraction(ctx, outsider.ID, "select-team-materials"); err == nil {
		t.Fatal("another principal reused the owner's card")
	}
	selectInput := f.click(t, "team.materials", "team.materials.select", "owner", map[string]any{"materials_mode": "existing", "materials_url": link})
	if _, err := r.service.SelectTeamMaterialsFromInteraction(ctx, selectInput.ID, "select-team-materials"); err != nil {
		t.Fatal(err)
	}
	platform := &teamMaterialsIntegrationPlatform{grants: map[string]bool{}}
	r.teamMaterials = workteamhome.New(r.store, func(context.Context, work.RoleBotBinding) (workteamhome.Platform, error) { return platform, nil })
	if err := r.teamMaterials.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if platform.opened != link || !platform.grants["chat"] || platform.grants[secondChat] {
		t.Fatalf("home or authorized audience differs: %+v", platform)
	}
	assertSecond := func(shared bool) {
		t.Helper()
		if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
			facts, err := teamProjectFacts(tx, secondProject)
			if err != nil {
				return err
			}
			rendered := strings.Join(teamProjectPanel(facts).Paragraphs, "\n")
			if strings.Contains(rendered, link) != shared {
				t.Fatalf("shared=%v, status=%s", shared, rendered)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertSecond(false)

	queue("share-team-materials", secondScope, secondChat, true)
	var share workstore.HumanInteraction
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "owner-group-message", Tenant: "tenant", SenderTenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "owner"}, ChatID: secondChat, ChatType: "group", MessageID: "owner-observed", MessageType: "text", SenderType: "user", RawContent: `{"text":"Share the team original here."}`, ReceivedAt: f.now, MessageCreatedAt: f.now}
		record, _, err := tx.AcceptRawIngress(raw, gateway)
		if err != nil {
			return err
		}
		if err = tx.GrantProjectAccessFromIngress(secondScope, f.owner, record.ID, f.now.AddDate(0, 0, 1)); err != nil {
			return err
		}
		view, err := tx.GetUIView("share-team-materials")
		if err != nil {
			return err
		}
		share, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "owner-requested-share", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "owner"}, ChatID: secondChat, MessageID: view.ExternalMessageID, Kind: "card", Values: map[string]any{"team_view": view.ID, "action": "team.materials.share"}, ReceivedAt: f.now}, gateway)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.service.ShareTeamMaterialsFromInteraction(ctx, share.ID, "share-team-materials"); err != nil {
		t.Fatal(err)
	}
	if err := r.teamMaterials.Run(ctx); err != nil {
		t.Fatal(err)
	}
	assertSecond(true)
	if !platform.grants[secondChat] || platform.opened != link {
		t.Fatalf("explicit share selected a copy or missed this project: %+v", platform)
	}

	if _, err := r.service.SelectTeamMaterialsFromInteraction(ctx, selectInput.ID, "select-team-materials"); err == nil || !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("old selection should be fenced after generation advances: %v", err)
	}
}
