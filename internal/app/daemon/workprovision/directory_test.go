package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

type directoryPlatformFake struct {
	*fakePlatform
	parent                     string
	moves                      int
	moveURL                    string
	tabUpdates, messageUpdates int
}

func (p *directoryPlatformFake) ResolveDirectory(_ context.Context, value string) (DirectoryTarget, error) {
	return DirectoryTarget{Kind: "drive", ID: value[strings.LastIndex(value, "/")+1:]}, nil
}
func (p *directoryPlatformFake) IsDocumentInDirectory(_ context.Context, id, _, _ string, target DirectoryTarget) (bool, error) {
	return id == p.doc.ID && p.parent == target.ID, nil
}
func (p *directoryPlatformFake) MoveDocument(_ context.Context, id, _ string, target DirectoryTarget) (feishu.ProjectDocumentMoveReceipt, error) {
	p.moves++
	if id != p.doc.ID {
		return feishu.ProjectDocumentMoveReceipt{}, errors.New("another home object")
	}
	p.parent = target.ID
	if p.moveURL != "" {
		p.doc.URL = p.moveURL
	}
	return feishu.ProjectDocumentMoveReceipt{DocumentID: id, RequestID: "actual-move"}, nil
}

type directoryCardSender struct{}

func (directoryCardSender) Resolve(context.Context, work.RoleBotBinding) (workpublish.Sender, error) {
	return directoryCardSender{}, nil
}
func (directoryCardSender) PublishText(context.Context, feishu.ProjectPublishRequest) (feishu.ProjectPublishReceipt, error) {
	panic("directory view must be a real card")
}
func (directoryCardSender) PublishCard(_ context.Context, req feishu.ProjectCardPublishRequest) (feishu.ProjectPublishReceipt, error) {
	return feishu.ProjectPublishReceipt{MessageID: "actual-directory-card", ChatID: req.ChatID}, nil
}
func installDirectoryPlatform(t *testing.T, f *provisionFixture) *directoryPlatformFake {
	t.Helper()
	p := &directoryPlatformFake{fakePlatform: f.leader, parent: "folder-created"}
	old := f.options.Resolver
	f.options.Resolver = ResolverFunc(func(ctx context.Context, b work.RoleBotBinding) (Platform, error) {
		if b.RoleID == "leader" {
			return p, nil
		}
		return old.Resolve(ctx, b)
	})
	var err error
	f.service, err = New(f.store, f.options)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func changeDirectoryFromActualForm(t *testing.T, f *provisionFixture, identity work.IdentityEvidence) error {
	t.Helper()
	const viewID = "directory-view"
	container := "https://a.feishu.cn/drive/folder/new-parent"
	err := f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		project, e := tx.GetProject(f.projectID)
		if e != nil {
			return e
		}
		home, e := tx.GetProjectHome(f.projectID)
		if e != nil {
			return e
		}
		op, e := tx.GetOperation(f.operationID)
		if e != nil {
			return e
		}
		target := workservice.ProjectDirectoryInteractionTarget{ProjectID: project.ID, ExpectedProjectRevision: project.Revision, ExpectedDirectoryGeneration: home.DirectoryGeneration}
		raw, _ := json.Marshal(target)
		if e = tx.PutContent(work.NewContent(viewID+":directory_target", f.scopeID, "application/json", raw, f.now)); e != nil {
			return e
		}
		card, e := workui.Card(viewID, workui.Panel{Title: "更换资料目录", Buttons: []workui.Button{{Label: "保存新目录并核对迁移", Action: "project.directory.select"}}})
		if e != nil {
			return e
		}
		_, e = workui.Queue(tx, workstore.UIView{ID: viewID, ScopeID: f.scopeID, SurfaceKey: "project.directory", Kind: "project.directory", TargetID: project.ID, TargetRevision: project.Revision, Binding: f.leader.binding, Tenant: "tenant", ChatID: "project-chat", ChannelGeneration: 1, PrincipalID: op.Actor.ID}, card)
		return e
	})
	if err != nil {
		return err
	}
	publisher, err := workpublish.New(f.store, directoryCardSender{}, func() time.Time { return f.now })
	if err != nil {
		return err
	}
	if _, err = publisher.RunOnce(t.Context(), 10); err != nil {
		return err
	}
	var interaction workstore.HumanInteraction
	err = f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		var e error
		gateway := work.IngressGateway{GatewayRef: "gateway-leader", AppID: "app-leader", RoleID: "leader", BotGeneration: 1}
		interaction, _, e = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "actual-directory-form", Tenant: "tenant", SenderIDs: identity.IDs, ChatID: "project-chat", MessageID: "actual-directory-card", Kind: "card", Values: map[string]any{"team_view": viewID, "action": "project.directory.select"}, Form: map[string]any{"document_container": container}, ReceivedAt: f.now}, gateway)
		if e != nil {
			return e
		}
		queried := work.IdentityScope{Tenant: "tenant", AppIdentityDomain: gateway.AppID, IDType: "open_id", ExternalID: identity.IDs.OpenID, Generation: 1}
		query := workstore.MembershipQuery{ScopeID: f.scopeID, PrincipalID: interaction.PrincipalID, Tenant: "tenant", ChatID: "project-chat", BindingGeneration: 1, QueriedIdentity: queried}
		raw, _ := json.Marshal(query)
		body := work.NewContent("directory-membership:request", f.scopeID, "application/json", raw, f.now)
		response := work.NewContent("directory-membership:response", f.scopeID, "application/json", []byte(`{"is_in_chat":true}`), f.now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		if e = tx.PutContent(response); e != nil {
			return e
		}
		op := work.Operation{ID: "directory-membership", ScopeID: f.scopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "membership-verifier"}, Kind: "feishu.membership.query", TargetID: "project-chat", TargetGeneration: 1, IdempotencyKey: "directory-membership", RequestRef: body.ID, RequestHash: body.SHA256, ExternalReceipt: response.ID, State: work.OperationConfirmed, Revision: 1, CreatedAt: f.now, UpdatedAt: f.now}
		if _, _, e = tx.PutOperation(op, 0); e != nil {
			return e
		}
		return tx.RecordMembershipCheck(work.MembershipCheck{ID: "directory-membership-check", ScopeID: f.scopeID, PrincipalID: interaction.PrincipalID, Gateway: gateway, Tenant: "tenant", ChatID: "project-chat", BindingGeneration: 1, QueriedIdentity: queried, Status: work.MembershipPresent, OperationID: op.ID, ResponseRef: response.ID, ObservedAt: f.now, ExpiresAt: f.now.Add(time.Hour)})
	})
	if err != nil {
		return err
	}
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return f.now })
	if err != nil {
		return err
	}
	service, err := workservice.New(f.store, signer, func() time.Time { return f.now })
	if err != nil {
		return err
	}
	result, err := service.SelectProjectDirectoryFromInteraction(t.Context(), interaction.ID, viewID)
	if err == nil && (result.Status != "selection_saved" || result.Revision != 1) {
		t.Fatalf("selection %+v", result)
	}
	return err
}
func TestDirectoryActualHumanFormFencesPausedCreateAndMovesSameLateDocument(t *testing.T) {
	identity := work.IdentityEvidence{Tenant: "tenant", AppID: "app-leader", Generation: 1, IDs: work.SenderIDs{OpenID: "actual-creator"}}
	f := newFixtureForSDK(t, &identity)
	platform := installDirectoryPlatform(t, f)
	f.leader.afterCreate = func() {
		if err := changeDirectoryFromActualForm(t, f, identity); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.Run(t.Context(), f.operationID); !errors.Is(err, ErrStale) {
		t.Fatalf("old-generation create continued: %v", err)
	}
	if f.leader.tabCalls != 0 || f.leader.publishCalls != 0 || f.leader.pinCalls != 0 || f.prepared != 0 {
		t.Fatal("old selection continued navigation or native preparation")
	}
	err := f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		home, e := tx.GetProjectHome(f.projectID)
		if e != nil {
			return e
		}
		if home.DirectoryGeneration != 1 || home.DirectoryAppliedGeneration != 0 || home.DocumentID != "" {
			t.Fatalf("late old receipt overwrote new selection %+v", home)
		}
		old, e := tx.GetProvisionStep(f.operationID, "materials.document")
		if e == nil && old.ExternalID != "document-created" {
			t.Fatal("actual late document ID was discarded")
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := f.service.Run(t.Context(), f.operationID)
	if err != nil || report.State != "confirmed" || platform.moves != 1 || f.leader.documentCalls != 1 {
		t.Fatalf("same-object migration failed %+v %v moves%d creates%d", report, err, platform.moves, f.leader.documentCalls)
	}
	err = f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		home, e := tx.GetProjectHome(f.projectID)
		if e == nil && (home.DocumentID != "document-created" || home.ContainerID != "new-parent" || home.DirectoryAppliedGeneration != 1) {
			t.Fatalf("new selection not actually applied %+v", home)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestDirectorySelectionWithUnknownCreationWithoutIDNeverCreatesAgain(t *testing.T) {
	identity := work.IdentityEvidence{Tenant: "tenant", AppID: "app-leader", Generation: 1, IDs: work.SenderIDs{OpenID: "actual-creator"}}
	f := newFixtureForSDK(t, &identity)
	platform := installDirectoryPlatform(t, f)
	f.leader.createErr = context.DeadlineExceeded
	if _, err := f.service.Run(t.Context(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if err := changeDirectoryFromActualForm(t, f, identity); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := f.service.Run(t.Context(), f.operationID); err == nil {
			t.Fatal("unknown create without ID was silently settled")
		}
	}
	if f.leader.documentCalls != 1 || platform.moves != 0 {
		t.Fatal("new selection blindly recreated or moved an unknown object")
	}
	if err := f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		home, e := tx.GetProjectHome(f.projectID)
		if e == nil && (home.DirectoryGeneration != 1 || home.DirectoryAppliedGeneration != 0) {
			t.Fatal("new selection was lost or falsely completed")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

func (p *directoryPlatformFake) UpdateDirectoryTab(_ context.Context, _ string, id, link string) error {
	p.tabUpdates++
	for i := range p.tabs {
		if p.tabs[i].ID == id {
			p.tabs[i].URL = link
			return nil
		}
	}
	return errors.New("recorded Tab absent")
}
func (p *directoryPlatformFake) UpdateDirectoryMessage(_ context.Context, id, text string) error {
	p.messageUpdates++
	p.messages[id] = text
	return nil
}
func TestDirectoryKnownHomeMovesInPlaceAndUpdatesItsExistingNavigation(t *testing.T) {
	identity := work.IdentityEvidence{Tenant: "tenant", AppID: "app-leader", Generation: 1, IDs: work.SenderIDs{OpenID: "actual-creator"}}
	f := newFixtureForSDK(t, &identity)
	platform := installDirectoryPlatform(t, f)
	f.leader.appendErr = errors.New("initial managed index incomplete")
	if _, err := f.service.Run(t.Context(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if f.leader.tabCalls != 1 || f.leader.pinCalls != 1 {
		t.Fatal("fixture has no actual fixed navigation")
	}
	platform.moveURL = "https://a.feishu.cn/wiki/actual-moved-node"
	if err := changeDirectoryFromActualForm(t, f, identity); err != nil {
		t.Fatal(err)
	}
	// The pre-existing index write remains unknown; directory relocation itself
	// can still finish without repeating that independent uncertain write.
	if _, err := f.service.Run(t.Context(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if platform.moves != 1 || platform.tabUpdates != 1 || platform.messageUpdates != 1 || f.leader.documentCalls != 1 || f.leader.tabCalls != 1 || f.leader.pinCalls != 1 {
		t.Fatalf("fixed entrance was duplicated/missing update: moves%d tabs%d updates%d messages%d pins%d", platform.moves, f.leader.tabCalls, platform.tabUpdates, platform.messageUpdates, f.leader.pinCalls)
	}
	if err := f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		home, e := tx.GetProjectHome(f.projectID)
		if e == nil && (home.DocumentID != "document-created" || home.URL != platform.moveURL || home.TabID != "system-project-tab" || home.NavigationMessageID != "navigation-message") {
			t.Fatalf("stable navigation changed %+v", home)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEstablishmentArchiveDuringCreateKeepsReceiptAndStopsLaterEffects(t *testing.T) {
	f := newFixture(t)
	f.leader.afterCreate = func() {
		if err := f.store.Within(t.Context(), func(tx *workstore.Tx) error {
			project, err := tx.GetProject(f.projectID)
			if err != nil {
				return err
			}
			old := project.Revision
			project.State = work.ProjectArchiving
			project.Revision++
			return tx.PutProject(project, old)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.Run(t.Context(), f.operationID); !errors.Is(err, ErrCancelled) {
		t.Fatalf("archive did not stop old establishment: %v", err)
	}
	if f.leader.documentCalls != 1 || f.leader.tabCalls != 0 || f.leader.publishCalls != 0 || f.leader.pinCalls != 0 || f.leader.appendCalls != 0 || f.prepared != 0 {
		t.Fatal("archived establishment created later navigation/index/native effects")
	}
	invites := len(f.world.inviteCalls)
	if _, err := f.service.Run(t.Context(), f.operationID); err == nil {
		t.Fatal("archiving establishment restarted")
	}
	if len(f.world.inviteCalls) != invites || f.leader.documentCalls != 1 {
		t.Fatal("stopped establishment repeated member/create writes")
	}
	if err := f.store.Within(t.Context(), func(tx *workstore.Tx) error {
		step, e := tx.GetProvisionStep(f.operationID, "materials.document")
		if e != nil {
			return e
		}
		home, e := tx.GetProjectHome(f.projectID)
		if e == nil && (step.ExternalID != "document-created" || home.DocumentID != "document-created" || len(step.Receipt) == 0) {
			t.Fatal("late external create receipt or retained resource was discarded")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}
