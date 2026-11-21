package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhttp"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/teamcli"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func useDeliveryJourneyGateway(t *testing.T, a *App, url string, identity work.IngressGateway) {
	t.Helper()
	controller := feishu.NewMultiGatewayController()
	if err := controller.UpsertApp(t.Context(), feishu.GatewayAppConfig{GatewayID: identity.GatewayRef, AppID: identity.AppID, AppSecret: "fixture-only", BotOpenID: "ou_bot", Domain: url, Enabled: true, ImageTempDir: t.TempDir(), PreviewCacheDir: t.TempDir(), PreviewStatePath: filepath.Join(t.TempDir(), "preview.json")}); err != nil {
		t.Fatal(err)
	}
	gatewayCtx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = controller.Start(gatewayCtx, nil) }()
	t.Cleanup(func() { cancel(); <-stopped })
	for deadline := time.Now().Add(3 * time.Second); ; {
		if _, err := controller.ProjectGateway(identity.GatewayRef, identity.AppID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test platform adapter unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	a.gateway = controller
	r := a.teamRuntime
	var err error
	r.publisher, err = workpublish.New(r.store, teamPublisherResolver{directory: teamDirectory{app: a, store: r.store}}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func newDeliveryJourneyCLI(t *testing.T, f *resourceRecoveryFixture, dir string) func(string, any, any) error {
	return newDeliveryJourneyCLIWithInput(t, f, dir, "publisher", "")
}

func newDeliveryJourneyCLIWithInput(t *testing.T, f *resourceRecoveryFixture, dir, label, input string) func(string, any, any) error {
	t.Helper()
	r := f.a.teamRuntime
	if label == "publisher" {
		r.service.WithArtifactHost("local-delivery-host")
	}
	attempt := startDeliveryJourneyRun(t, r.store, dir, label, input)
	token, err := f.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: "scope", ContextID: attempt.ContextID, WorkerID: attempt.RunRef.WorkerID, SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(workhttp.NewHandler(r.service))
	t.Cleanup(endpoint.Close)
	return func(command string, payload any, result any) error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		var output bytes.Buffer
		err = teamcli.Run(t.Context(), teamcli.Options{Args: []string{command, "--run-ref", attempt.RunRef.RunID, "--stdin"}, Stdin: bytes.NewReader(data), Stdout: &output, Getenv: func(key string) string {
			return map[string]string{config.WorkToolEndpointEnv: endpoint.URL, config.WorkSessionTokenEnv: token}[key]
		}})
		if err != nil {
			return err
		}
		return json.Unmarshal(output.Bytes(), result)
	}
}

func TestDeliveryJourneyCLIToNativeDocxShareAndRetainedOriginal(t *testing.T) {
	ctx := t.Context()
	appID := isolatedSDKFixtureAppID(t, "delivery-docx")
	f := newResourceRecoveryFixture(t, appID)
	a := f.a
	identity := work.IngressGateway{GatewayRef: "gateway", AppID: appID, RoleID: "leader", BotGeneration: 1}
	tenantToken := "fixture-token-" + appID
	const docPath = "/open-apis/docx/v1/documents/newdoc"
	const permissionPath = "/open-apis/drive/v1/permissions/newdoc/members"
	var docCreates, revision, grants atomic.Int64
	var groupCanView atomic.Bool
	var accessReadUnavailable atomic.Bool
	var notices atomic.Int64
	revision.Store(1)
	docBlock := func(rev int64) string {
		text := "原始说明"
		if rev == 3 {
			text = "已缩短说明"
		}
		return fmt.Sprintf(`{"block_id":"managed-line","block_type":2,"text":{"elements":[{"text_run":{"content":%q}},{"text_run":{"content":"他人补充"}}]}}`, text)
	}
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		if strings.HasPrefix(path, "/open-apis/") && path != "/open-apis/auth/v3/tenant_access_token/internal" && r.Header.Get("Authorization") != "Bearer "+tenantToken {
			t.Error("Feishu connector lost bound application token")
		}
		switch {
		case path == "/open-apis/auth/v3/tenant_access_token/internal":
			fmt.Fprintf(w, `{"code":0,"tenant_access_token":%q,"expire":7200}`, tenantToken)
		case path == "/open-apis/docx/v1/documents" && r.Method == http.MethodPost:
			docCreates.Add(1)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"folder_token":"project-folder"`) {
				t.Errorf("unexpected creation directory %s", body)
			}
			fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"newdoc","revision_id":1}}}`)
		case path == docPath:
			fmt.Fprintf(w, `{"code":0,"data":{"document":{"document_id":"newdoc","revision_id":%d}}}`, revision.Load())
		case path == docPath+"/blocks/newdoc":
			fmt.Fprint(w, `{"code":0,"data":{"block":{"block_id":"newdoc","block_type":1,"page":{"elements":[]}}}}`)
		case path == docPath+"/blocks/newdoc/children" && r.Method == http.MethodPost:
			revision.Store(2)
			fmt.Fprintf(w, `{"code":0,"data":{"document_revision_id":2,"children":[%s]}}`, docBlock(2))
		case path == docPath+"/blocks/managed-line":
			fmt.Fprintf(w, `{"code":0,"data":{"block":%s}}`, docBlock(revision.Load()))
		case path == docPath+"/blocks" && r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"code":0,"data":{"items":[%s],"has_more":false}}`, docBlock(revision.Load()))
		case path == docPath+"/blocks/batch_update" && r.Method == http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "已缩短说明") || !strings.Contains(string(body), "他人补充") || r.URL.Query().Get("document_revision_id") != "2" {
				t.Errorf("in-place revision would erase unrelated human edit or lose baseline: %s %s", r.URL, body)
			}
			revision.Store(3)
			fmt.Fprintf(w, `{"code":0,"data":{"document_revision_id":3,"blocks":[%s]}}`, docBlock(3))
		case path == permissionPath && r.Method == http.MethodGet:
			if accessReadUnavailable.Load() {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"code":123,"msg":"ACL read temporarily unavailable"}`)
			} else if groupCanView.Load() {
				fmt.Fprint(w, `{"code":0,"data":{"items":[{"member_id":"chat","member_type":"openchat","type":"chat","perm":"view"}]}}`)
			} else {
				fmt.Fprint(w, `{"code":0,"data":{"items":[]}}`)
			}
		case path == permissionPath && r.Method == http.MethodPost:
			count := grants.Add(1)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"member_id":"chat"`) || !strings.Contains(string(body), `"member_type":"openchat"`) || !strings.Contains(string(body), `"perm":"view"`) {
				t.Errorf("grant escaped the intended group or permission: %s", body)
			}
			if count == 1 {
				w.WriteHeader(http.StatusBadGateway)
				fmt.Fprint(w, `{"code":123,"msg":"write response lost"}`)
			} else {
				groupCanView.Store(true)
				fmt.Fprint(w, `{"code":0,"data":{}}`)
			}
		case path == "/open-apis/im/v1/messages" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			hasOriginal := strings.Contains(string(body), `https://a.feishu.cn/docx/newdoc`) && strings.Contains(string(body), `open_url`)
			if hasOriginal != groupCanView.Load() {
				t.Errorf("delivery wrongly represented the original's access: %s", body)
			}
			fmt.Fprintf(w, `{"code":0,"data":{"message_id":"om_docx_%d","chat_id":"chat"}}`, notices.Add(1))
		case path == "/open-apis/im/v1/chats/chat/members":
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"member_id":"owner","member_id_type":"open_id","tenant_key":"tenant"}],"has_more":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(platform.Close)
	useDeliveryJourneyGateway(t, a, platform.URL, identity)
	store := a.teamRuntime.store
	service, err := workservice.NewWithResourceConnectorResolver(store, f.signer, time.Now, teamResourceResolver{directory: teamDirectory{app: a, store: store}})
	if err != nil {
		t.Fatal(err)
	}
	a.teamRuntime.service = service
	pickerTx(t, a, func(tx *workstore.Tx) error {
		return tx.PutResource(work.Resource{ID: "project-parent", ScopeID: "scope", ConnectorIdentity: f.resource.ConnectorIdentity, SourceIdentity: "folder:project-folder", CanonicalURI: "https://a.feishu.cn/drive/folder/project-folder", CollectionID: "materials", Purpose: "Project folder", Revision: 1}, 0)
	})
	cli := newDeliveryJourneyCLI(t, f, t.TempDir())
	var created, appended, updated workservice.ResourceWriteResult
	if err := cli("resource.document.create", workservice.CreateResourceDocumentRequest{Key: "newdoc", ParentResourceID: "project-parent", ExpectedParentRevision: 1, Title: "发布说明", Purpose: "发布说明", CollectionID: "materials"}, &created); err != nil || created.Status != "created" || created.ObjectID == "" {
		t.Fatalf("created doc %+v %v", created, err)
	}
	if err := cli("resource.document.append", workservice.AppendResourceDocumentRequest{Key: "append-doc", ResourceID: created.ObjectID, ExpectedResourceRevision: 1, ObservedDocumentRevision: 1, ParentBlockID: "newdoc", Paragraphs: []string{"原始说明"}}, &appended); err != nil || appended.Status != "written" {
		t.Fatalf("append doc %+v %v", appended, err)
	}
	share := workservice.ShareProjectDocumentRequest{ResourceID: created.ObjectID}
	var access workservice.Result
	for range 2 {
		if err := cli("resource.document.share-project", share, &access); err != nil || access.Status != "pending_external" || grants.Load() != 1 {
			t.Fatalf("unconfirmed permission did not preserve a single grant: %+v grants=%d %v", access, grants.Load(), err)
		}
	}
	pub := workservice.PublishDeliveryRequest{Key: "current-doc", Purpose: "发布说明", Summary: "请查看发布说明", Audience: []work.Actor{{Kind: work.ActorRole, ID: "leader"}}, Retention: work.RetentionRetained, References: []work.DeliveryRef{{ResourceID: created.ObjectID, Current: true}}}
	var delivery workservice.Result
	if err := cli("delivery.publish", pub, &delivery); err == nil {
		t.Fatal("claimed a current-only delivery before group access was verified")
	}
	groupCanView.Store(true)
	if err := cli("resource.document.share-project", share, &access); err != nil || access.Status != "project_access_confirmed" || grants.Load() != 1 {
		t.Fatalf("read-only reconciliation %+v grants=%d %v", access, grants.Load(), err)
	}
	var old, next workservice.Result
	if err := cli("resource.capture", workservice.CaptureResourceRequest{Key: "capture-doc-before", ResourceID: created.ObjectID}, &old); err != nil || old.ObjectID == "" {
		t.Fatalf("first document version %+v %v", old, err)
	}
	var observed workservice.ResourceDocumentInspection
	if err := cli("resource.document.inspect", workservice.InspectResourceDocumentRequest{ResourceID: created.ObjectID, BlockID: "managed-line"}, &observed); err != nil || len(observed.Blocks) != 1 {
		t.Fatalf("inspection %+v %v", observed, err)
	}
	if err := cli("resource.document.update", workservice.UpdateResourceDocumentRequest{Key: "revise-doc", ResourceID: created.ObjectID, ExpectedResourceRevision: 1, ObservedDocumentRevision: observed.ObservedRevision, Edits: []workservice.ResourceTextRunEdit{{BlockID: "managed-line", TextRunIndex: 0, ExpectedBlockSHA256: observed.Blocks[0].SHA256, Text: "已缩短说明"}}}, &updated); err != nil || updated.Status != "written" || updated.ObjectID != created.ObjectID || docCreates.Load() != 1 {
		t.Fatalf("in-place revision %+v %v", updated, err)
	}
	if err := cli("resource.capture", workservice.CaptureResourceRequest{Key: "capture-doc-after", ResourceID: created.ObjectID}, &next); err != nil || next.ObjectID == old.ObjectID {
		t.Fatalf("second document version %+v %v", next, err)
	}
	for _, snapshot := range []struct{ id, expected string }{{old.ObjectID, "原始说明"}, {next.ObjectID, "已缩短说明"}} {
		var saved workservice.ResourceVersionView
		if err := cli("resource.read-version", workservice.ReadResourceVersionRequest{ResourceID: created.ObjectID, VersionID: snapshot.id}, &saved); err != nil || saved.Content == nil || !strings.Contains(saved.Content.Text, snapshot.expected) || !strings.Contains(saved.Content.Text, "他人补充") {
			t.Fatalf("wrong retained document snapshot %+v %v", saved, err)
		}
	}
	pub.References = append(pub.References, work.DeliveryRef{ResourceID: created.ObjectID, VersionID: next.ObjectID})
	if err := cli("delivery.publish", pub, &delivery); err != nil || delivery.Status != "accepted" {
		t.Fatalf("document delivery %+v %v", delivery, err)
	}
	if count, err := a.teamRuntime.publisher.RunOnce(ctx, 10); count != 1 || err != nil || notices.Load() != 1 {
		t.Fatalf("document did not reach Feishu publication %d %v", count, err)
	}
	deliveredID := delivery.ObjectID
	originalOperation := access.OperationID
	groupCanView.Store(false)
	pub.Key = "withdrawn-current-original"
	pub.References = []work.DeliveryRef{{ResourceID: created.ObjectID, Current: true}}
	if err := cli("delivery.publish", pub, &delivery); err == nil {
		t.Fatal("revoked access was accepted as a current-only delivery")
	}
	if err := cli("resource.document.share-project", share, &access); err != nil || access.Status != "project_access_revoked" || grants.Load() != 1 {
		t.Fatalf("revocation failed to stop automatic regrant: %+v %v", access, err)
	}
	share.SourceMessageID = "fabricated-restoration"
	if err := cli("resource.document.share-project", share, &access); err == nil || grants.Load() != 1 {
		t.Fatalf("forged source could restore access: %+v %v", access, err)
	}
	pub.Key = "retained-even-when-group-access-withdrawn"
	pub.References = []work.DeliveryRef{{ResourceID: created.ObjectID, VersionID: old.ObjectID}}
	if err := cli("delivery.publish", pub, &delivery); err != nil || delivery.Status != "accepted" {
		t.Fatalf("failed to retain access to the saved version: %+v %v", delivery, err)
	}
	if count, err := a.teamRuntime.publisher.RunOnce(ctx, 10); count != 1 || err != nil || notices.Load() != 2 {
		t.Fatalf("saved-only delivery did not publish: %d %v", count, err)
	}
	requestMsg, input := receiveDocAccessRestoration(t, f, identity)
	accessReadUnavailable.Store(true)
	assertDocAccessFailureKeepsFrozenReader(t, f, identity, deliveredID)
	accessReadUnavailable.Store(false)
	ownerCLI := newDeliveryJourneyCLIWithInput(t, f, t.TempDir(), "restore-access", input.ID)
	share.SourceMessageID = requestMsg.ID
	if err := ownerCLI("resource.document.share-project", share, &access); err != nil || access.Status != "project_access_confirmed" || grants.Load() != 2 || access.OperationID == originalOperation {
		t.Fatalf("new admitted instruction did not independently restore access: %+v %v", access, err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		previous, err := tx.GetOperation(originalOperation)
		if err != nil {
			return err
		}
		if previous.State != work.OperationConfirmed || previous.ExternalReceipt == "" {
			return fmt.Errorf("initial grant receipt was overwritten: %+v", previous)
		}
		return nil
	})
}

func assertDocAccessFailureKeepsFrozenReader(t *testing.T, f *resourceRecoveryFixture, identity work.IngressGateway, deliveryID string) {
	t.Helper()
	ctx := t.Context()
	var interaction workstore.HumanInteraction
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		views, err := tx.ListUIViews("scope")
		if err != nil {
			return err
		}
		for _, view := range views {
			if view.TargetID == deliveryID && view.ExternalMessageID != "" {
				interaction, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: identity.GatewayRef, AppID: identity.AppID, EventID: "read-old-doc", Tenant: "tenant", ChatID: "chat", MessageID: view.ExternalMessageID, SenderIDs: work.SenderIDs{OpenID: "owner"}, Kind: "card", Values: map[string]any{"team_view": view.ID, "action": "artifacts.open"}, ReceivedAt: time.Now().UTC()}, identity)
				return err
			}
		}
		return fmt.Errorf("old delivery card has no confirmed receipt")
	})
	if err := f.a.processTeamInteraction(ctx, interaction); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		finished, err := tx.GetHumanInteraction(interaction.ID)
		if err != nil {
			return err
		}
		reader, err := tx.GetUIView(finished.ResultRef)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(reader.ScopeID, reader.BodyRef)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body.Data), "已缩短说明") || !strings.Contains(string(body.Data), "待核对或恢复") || strings.Contains(string(body.Data), "https://a.feishu.cn/docx/newdoc") {
			return fmt.Errorf("failed live check hid snapshot or showed an unconfirmed original: %s", body.Data)
		}
		return nil
	})
}

func receiveDocAccessRestoration(t *testing.T, f *resourceRecoveryFixture, identity work.IngressGateway) (work.Message, work.Inbox) {
	t.Helper()
	ctx := t.Context()
	store := f.a.teamRuntime.store
	ingress, err := workingress.New(store, workingress.Config{Directory: deliveryJourneyDirectory{identity}})
	if err != nil {
		t.Fatal(err)
	}
	const messageID = "owner-restore-project-original"
	_, err = ingress.Receive(ctx, work.RawIngressEnvelope{GatewayRef: identity.GatewayRef, AppID: identity.AppID, EventID: "restore-event", Tenant: "tenant", ChatID: "chat", ChatType: "group", MessageID: messageID, SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "owner"}, MessageType: "text", RawContent: `{"text":"请恢复本群查看发布说明原件的权限"}`, ReceivedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ingress.ProcessPending(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var source work.Message
	var input work.Inbox
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		inputs, err := tx.ListPendingInputs("scope", "leader")
		if err != nil {
			return err
		}
		for _, candidate := range inputs {
			if candidate.MessageID == "" {
				continue
			}
			message, err := tx.GetMessage(candidate.MessageID)
			if err != nil {
				return err
			}
			if message.PlatformKey != nil && message.PlatformKey.MessageID == messageID {
				source, input = message, candidate
				return nil
			}
		}
		return fmt.Errorf("restoration instruction did not enter the project inbox")
	})
	return source, input
}
