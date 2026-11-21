package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

type recoveryReadConnector struct {
	createErr        error
	identity         string
	reads, writes    int
	entered, release chan struct{}
}

func (f *recoveryReadConnector) Identity() string { return f.identity }
func (f *recoveryReadConnector) Resolve(_ context.Context, url string) (workservice.ResolvedResource, error) {
	f.reads++
	if !strings.HasSuffix(url, "/docx/chosen") {
		return workservice.ResolvedResource{}, errors.New("another object")
	}
	return workservice.ResolvedResource{SourceIdentity: "docx:chosen", CanonicalURI: url, Source: work.SourceRef{Kind: "feishu_docx", ID: "chosen", Version: "9", EvidenceRef: "actual-sdk-read"}, Evidence: json.RawMessage(`{"document_id":"chosen","revision_id":9}`)}, nil
}
func (f *recoveryReadConnector) Capture(context.Context, work.Resource, workservice.CaptureResourceRequest) (workservice.CapturedResource, error) {
	panic("recovery must not publish snapshots")
}
func (f *recoveryReadConnector) CreateResourceDocument(context.Context, work.Resource, workservice.CreateResourceDocumentRequest) (workservice.ResourceWriteReceipt, error) {
	f.writes++
	close(f.entered)
	<-f.release
	return workservice.ResourceWriteReceipt{Attempted: true, ExternalID: "late-created", SourceIdentity: "docx:late-created", CanonicalURI: "https://a.feishu.cn/docx/late-created"}, f.createErr
}
func (f *recoveryReadConnector) InspectResourceDocument(_ context.Context, r work.Resource, req workservice.InspectResourceDocumentRequest) (workservice.ResourceDocumentInspection, error) {
	f.reads++
	return workservice.ResourceDocumentInspection{DocumentID: "document", ObservedRevision: 9, RequestedRevision: 9, Blocks: []workservice.ResourceDocumentBlock{{ID: req.BlockID, SHA256: strings.Repeat("a", 64), Runs: []workservice.ResourceDocumentRun{{Kind: "text", Summary: "actual current text"}}}}}, nil
}
func (f *recoveryReadConnector) AppendResourceDocument(context.Context, work.Resource, workservice.AppendResourceDocumentRequest, string) (workservice.ResourceWriteReceipt, error) {
	panic("recovery repeated append")
}
func (f *recoveryReadConnector) UpdateResourceDocument(context.Context, work.Resource, workservice.UpdateResourceDocumentRequest, string) (workservice.ResourceWriteReceipt, error) {
	panic("recovery repeated update")
}
func (f *recoveryReadConnector) ReadResourceSheet(context.Context, work.Resource, workservice.ReadResourceSheetRequest) (workservice.ResourceSheetInspection, error) {
	f.reads++
	return workservice.ResourceSheetInspection{DataSHA256: strings.Repeat("b", 64), Values: feishu.ProjectSheetValues{Range: "sheet!A1:B1", Values: [][]any{{"current", json.Number("9007199254740993")}}, Revision: json.RawMessage(`9`)}}, nil
}
func (f *recoveryReadConnector) WriteResourceSheet(context.Context, work.Resource, workservice.WriteResourceSheetRequest) (workservice.ResourceWriteReceipt, error) {
	panic("recovery repeated sheet write")
}

type recoveryUISender struct{}

func (recoveryUISender) Resolve(context.Context, work.RoleBotBinding) (workpublish.Sender, error) {
	return recoveryUISender{}, nil
}
func (recoveryUISender) PublishText(context.Context, feishu.ProjectPublishRequest) (feishu.ProjectPublishReceipt, error) {
	panic("expected card")
}
func (recoveryUISender) PublishCard(_ context.Context, r feishu.ProjectCardPublishRequest) (feishu.ProjectPublishReceipt, error) {
	return feishu.ProjectPublishReceipt{MessageID: "card-" + r.UUID, ChatID: r.ChatID}, nil
}

type resourceRecoveryFixture struct {
	a         *App
	connector *recoveryReadConnector
	signer    *workauth.Signer
	owner     string
	now       time.Time
	resource  work.Resource
}

func newResourceRecoveryFixture(t *testing.T) *resourceRecoveryFixture {
	t.Helper()
	a := teamProjectViewFixture(t)
	f := &resourceRecoveryFixture{a: a, now: time.Now().UTC()}
	f.connector = &recoveryReadConnector{identity: workservice.FeishuConnectorIdentity(work.RoleBotBinding{RoleID: "leader", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1})}
	var err error
	f.signer, err = workauth.New([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	a.teamRuntime.service, err = workservice.NewWithResourceConnectors(a.teamRuntime.store, f.signer, func() time.Time { return f.now }, map[string]workservice.ResourceConnector{"leader": f.connector})
	if err != nil {
		t.Fatal(err)
	}
	a.teamRuntime.publisher, err = workpublish.New(a.teamRuntime.store, recoveryUISender{}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	err = a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		initial, _, e := tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "actual-owner-source", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "owner"}, ChatID: "chat", MessageID: "actual-owner-card", Kind: "card", Values: map[string]any{"action": "initial-owner"}, ReceivedAt: f.now}, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "leader", BotGeneration: 1})
		if e != nil {
			return e
		}
		f.owner = initial.PrincipalID
		body := work.NewContent("original-project-request", "scope", "application/json", []byte(`{"principal_id":"`+f.owner+`"}`), f.now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: "project-provision", ScopeID: "scope", Actor: work.Actor{Kind: work.ActorPrincipal, ID: f.owner}, Kind: "project.provision", ExternalReceipt: "project-created", TargetID: "project", TargetGeneration: 1, IdempotencyKey: "project-provision", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationConfirmed, Revision: 1}, 0)
		if e != nil {
			return e
		}
		f.resource = work.Resource{ID: "target", ScopeID: "scope", ConnectorIdentity: f.connector.identity, SourceIdentity: "docx:document", CanonicalURI: "https://a.feishu.cn/docx/document", CollectionID: "materials", Purpose: "Existing work document", Revision: 1}
		return tx.PutResource(f.resource, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *resourceRecoveryFixture) unknown(t *testing.T, kind string) string {
	t.Helper()
	id := "unknown-" + kind
	err := f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		resource := f.resource
		intent := map[string]any{"kind": kind, "scope_id": "scope", "binding": work.RoleBotBinding{RoleID: "leader", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, "resource": resource}
		switch kind {
		case "resource.document.create":
			intent["create"] = workservice.CreateResourceDocumentRequest{Key: "create", ParentResourceID: resource.ID, ExpectedParentRevision: 1, Title: "Requested document", Purpose: "Work", CollectionID: "materials"}
		case "resource.document.append":
			intent["append"] = workservice.AppendResourceDocumentRequest{Key: "append", ResourceID: resource.ID, ExpectedResourceRevision: 1, ObservedDocumentRevision: 7, ParentBlockID: "document", Paragraphs: []string{"uncertain appended text"}}
		case "resource.document.update":
			intent["update"] = workservice.UpdateResourceDocumentRequest{Key: "update", ResourceID: resource.ID, ExpectedResourceRevision: 1, ObservedDocumentRevision: 7, Edits: []workservice.ResourceTextRunEdit{{BlockID: "block", TextRunIndex: 0, ExpectedBlockSHA256: strings.Repeat("c", 64), Text: "uncertain changed text"}}}
		case "resource.sheet.write":
			resource.ID = "sheet-target"
			resource.SourceIdentity = "sheets:spreadsheet"
			resource.CanonicalURI = "https://a.feishu.cn/sheets/spreadsheet"
			if e := tx.PutResource(resource, 0); e != nil {
				return e
			}
			intent["resource"] = resource
			intent["sheet"] = workservice.WriteResourceSheetRequest{Key: "write", ResourceID: resource.ID, ExpectedResourceRevision: 1, SheetID: "sheet", Range: "A1:B1", ExpectedDataSHA256: strings.Repeat("d", 64), Values: [][]any{{"uncertain", 1}}}
		}
		raw, _ := json.Marshal(intent)
		body := work.NewContent(id+":request", "scope", "application/json", raw, f.now)
		if e := tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: id, ScopeID: "scope", Actor: work.Actor{Kind: work.ActorRole, ID: "leader"}, Kind: kind, TargetID: resource.ID, TargetGeneration: 1, IdempotencyKey: id, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1}
		if _, _, e := tx.PutOperation(op, 0); e != nil {
			return e
		}
		op.State = work.OperationSending
		op.Revision++
		if _, _, e := tx.PutOperation(op, 1); e != nil {
			return e
		}
		op.State = work.OperationUnknown
		op.Revision++
		_, _, e := tx.PutOperation(op, 2)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *resourceRecoveryFixture) click(t *testing.T, kind, action, openID string, form map[string]any) workstore.HumanInteraction {
	t.Helper()
	if _, err := f.a.teamRuntime.publisher.RunOnce(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	var interaction workstore.HumanInteraction
	err := f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews("scope")
		if e != nil {
			return e
		}
		var view workstore.UIView
		for _, v := range views {
			if v.Kind == kind {
				view = v
			}
		}
		if view.ID == "" || view.ExternalMessageID == "" {
			return fmt.Errorf("actual recovery view not published: %s", kind)
		}
		interaction, _, e = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: view.ID + ":" + openID, Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: openID}, ChatID: "chat", MessageID: view.ExternalMessageID, Kind: "card", Values: map[string]any{"team_view": view.ID, "action": action}, Form: form, ReceivedAt: f.now}, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "leader", BotGeneration: 1})
		if e != nil {
			return e
		}
		if openID != "owner" {
			return nil
		}
		identity := work.IdentityScope{Tenant: "tenant", AppIdentityDomain: "app", IDType: "open_id", ExternalID: openID, Generation: 1}
		query := workstore.MembershipQuery{ScopeID: "scope", PrincipalID: interaction.PrincipalID, Tenant: "tenant", ChatID: "chat", BindingGeneration: 1, QueriedIdentity: identity}
		raw, _ := json.Marshal(query)
		id := "membership:" + interaction.ID
		body := work.NewContent(id+":request", "scope", "application/json", raw, f.now)
		response := work.NewContent(id+":response", "scope", "application/json", []byte(`{"is_in_chat":true}`), f.now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		if e = tx.PutContent(response); e != nil {
			return e
		}
		op := work.Operation{ID: id, ScopeID: "scope", Actor: work.Actor{Kind: work.ActorSystem, ID: "membership-verifier"}, Kind: "feishu.membership.query", TargetID: "chat", TargetGeneration: 1, IdempotencyKey: id, RequestRef: body.ID, RequestHash: body.SHA256, ExternalReceipt: response.ID, State: work.OperationConfirmed, Revision: 1, CreatedAt: f.now, UpdatedAt: f.now}
		if _, _, e = tx.PutOperation(op, 0); e != nil {
			return e
		}
		return tx.RecordMembershipCheck(work.MembershipCheck{ID: id + ":check", ScopeID: "scope", PrincipalID: interaction.PrincipalID, Gateway: interaction.Gateway, Tenant: "tenant", ChatID: "chat", BindingGeneration: 1, QueriedIdentity: identity, Status: work.MembershipPresent, OperationID: id, ResponseRef: response.ID, ObservedAt: f.now, ExpiresAt: f.now.Add(time.Hour)})
	})
	if err != nil {
		t.Fatal(err)
	}
	return interaction
}
func (f *resourceRecoveryFixture) inspectAndConfirm(t *testing.T) (workservice.ResourceWriteResult, workstore.HumanInteraction) {
	t.Helper()
	first := f.click(t, "resource.write.recovery", "resource.write.inspect", "owner", map[string]any{"source_url": "https://a.feishu.cn/docx/chosen"})
	viewID := first.Envelope.Values["team_view"].(string)
	inspection, err := f.a.teamRuntime.service.InspectResourceWriteRecovery(t.Context(), first.ID, viewID)
	if err != nil {
		t.Fatal(err)
	}
	var view workstore.UIView
	if err = f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error { var e error; view, e = tx.GetUIView(viewID); return e }); err != nil {
		t.Fatal(err)
	}
	if err = f.a.queueResourceRecoveryConfirmation(t.Context(), first, view, inspection); err != nil {
		t.Fatal(err)
	}
	second := f.click(t, "resource.write.recovery.confirm", "resource.write.adopt", "owner", nil)
	result, err := f.a.teamRuntime.service.AdoptResourceWriteRecovery(t.Context(), second.ID, second.Envelope.Values["team_view"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.FinishHumanInteraction(second.ID, second.Revision, result.OperationID)
	}); err != nil {
		t.Fatal(err)
	}
	return result, second
}
func TestResourceRecoveryActualUICurrentObservationAndHumanAdoptionAllowsArchive(t *testing.T) {
	for _, kind := range []string{"resource.document.create", "resource.document.append", "resource.document.update", "resource.sheet.write"} {
		t.Run(kind, func(t *testing.T) {
			f := newResourceRecoveryFixture(t)
			id := f.unknown(t, kind)
			if err := f.a.runTeamResourceWriteRecoveryViews(t.Context()); err != nil {
				t.Fatal(err)
			}
			result, _ := f.inspectAndConfirm(t)
			if result.Status != "adopted_current_state" || result.ObjectID == "" || f.connector.writes != 0 {
				t.Fatalf("recovery %+v writes%d", result, f.connector.writes)
			}
			err := f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(id)
				if e != nil {
					return e
				}
				sealed, e := tx.ResourceWriteWasRecovered(op)
				if e != nil {
					return e
				}
				if !sealed || op.State != work.OperationUnknown {
					t.Fatal("recovery forged original success or lacks human proof")
				}
				project, e := tx.GetProject("project")
				if e != nil {
					return e
				}
				old := project.Revision
				project.State = work.ProjectArchiving
				project.Revision++
				return tx.PutProject(project, old)
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.a.teamRuntime.service.ReconcileProjectLifecycle(t.Context(), "project"); err != nil {
				t.Fatal(err)
			}
			if err = f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
				project, e := tx.GetProject("project")
				if e == nil && project.State != work.ProjectArchived {
					t.Fatal("human-recovered unknown permanently blocked archive")
				}
				return e
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
