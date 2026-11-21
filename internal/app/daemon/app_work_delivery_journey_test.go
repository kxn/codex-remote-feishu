package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type deliveryJourneyDirectory struct{ identity work.IngressGateway }

func (d deliveryJourneyDirectory) ResolveGateway(context.Context, string, string) (work.IngressGateway, error) {
	return d.identity, nil
}
func (d deliveryJourneyDirectory) ResolveLeader(context.Context, string, string, string) (string, error) {
	return d.identity.RoleID, nil
}
func (d deliveryJourneyDirectory) ResolveMention(context.Context, work.IngressGateway, work.RawMention) (string, error) {
	return "", nil
}

func TestDeliveryJourneyCLIToFeishuFrozenFileAndOlderFeedback(t *testing.T) {
	ctx := t.Context()
	appID := isolatedSDKFixtureAppID(t, "delivery-file")
	f := newResourceRecoveryFixture(t, appID)
	a := f.a
	service, err := workservice.NewWithResourceConnectors(a.teamRuntime.store, f.signer, nil, map[string]workservice.ResourceConnector{"leader": f.connector})
	if err != nil {
		t.Fatal(err)
	}
	a.teamRuntime.service = service
	identity := work.IngressGateway{GatewayRef: "gateway", AppID: appID, RoleID: "leader", BotGeneration: 1}
	tenantToken := "fixture-token-" + appID
	oldFile := []byte("%PDF-1.7\ninitial release\x00\xff\n")
	newFile := []byte("%PDF-1.7\nupdated release\x00\xff\n")
	var deliveredCards atomic.Int64
	uploads := make(chan []byte, 1)
	fileReplies := make(chan string, 1)
	var holdUpload atomic.Bool
	uploadStarted, allowUpload := make(chan struct{}), make(chan struct{})
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			fmt.Fprintf(w, `{"code":0,"tenant_access_token":%q,"expire":7200}`, tenantToken)
		case r.URL.Path == "/open-apis/im/v1/chats/chat/members":
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"member_id":"owner","member_id_type":"open_id","tenant_key":"tenant"},{"member_id":"second-member","member_id_type":"open_id","tenant_key":"tenant"}],"has_more":false}}`)
		case r.URL.Path == "/open-apis/im/v1/files":
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(w, "missing file", http.StatusBadRequest)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil || r.FormValue("file_name") != "report.pdf" || header == nil {
				t.Errorf("unexpected uploaded file: %v", err)
			}
			uploads <- data
			if holdUpload.Load() {
				close(uploadStarted)
				select {
				case <-allowUpload:
				case <-r.Context().Done():
					return
				}
			}
			fmt.Fprint(w, `{"code":0,"data":{"file_key":"file_saved_version"}}`)
		case strings.HasPrefix(r.URL.Path, "/open-apis/im/v1/messages"):
			var body struct {
				Content string `json:"content"`
				Kind    string `json:"msg_type"`
				UUID    string `json:"uuid"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.UUID == "" || r.Header.Get("Authorization") != "Bearer "+tenantToken {
				t.Errorf("unexpected request %s %s: %#v %v", r.Method, r.URL.Path, body, err)
			}
			if body.Kind == "file" {
				if body.Content != `{"file_key":"file_saved_version"}` {
					t.Error(body.Content)
				}
				fileReplies <- r.URL.Path
			} else if body.Kind != "interactive" || !json.Valid([]byte(body.Content)) {
				t.Errorf("invalid result card: %#v", body)
			}
			fmt.Fprintf(w, `{"code":0,"data":{"message_id":"om_delivery_%d","chat_id":"chat"}}`, deliveredCards.Add(1))
		default:
			// The controlled endpoint deliberately has no WebSocket service.
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(platform.Close)
	useDeliveryJourneyGateway(t, a, platform.URL, identity)
	dir := t.TempDir()
	cli := newDeliveryJourneyCLI(t, f, dir)
	path := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(path, oldFile, 0600); err != nil {
		t.Fatal(err)
	}
	initial := workservice.SaveResourceFileRequest{Key: "draft-one", Path: "report.pdf", Purpose: "导出报告", CollectionID: "deliverables"}
	var first, second, replay workservice.SavedResourceFile
	if err := cli("resource.file.save", initial, &first); err != nil || first.ResourceID == "" || first.VersionID == "" || first.Revision != 1 {
		t.Fatalf("first save %+v: %v", first, err)
	}
	unsafe := initial
	unsafe.Key, unsafe.Path = "absolute", path
	if err := cli("resource.file.save", unsafe, &replay); err == nil {
		t.Fatal("absolute path accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.pdf")
	if err := os.WriteFile(outside, oldFile, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.pdf")); err == nil {
		unsafe := initial
		unsafe.Key, unsafe.Path = "escape", "escape.pdf"
		if err := cli("resource.file.save", unsafe, &replay); err == nil {
			t.Fatal("symlink outside execution location accepted")
		}
	}
	pub := func(key string, saved workservice.SavedResourceFile) workservice.Result {
		t.Helper()
		var value workservice.Result
		req := workservice.PublishDeliveryRequest{Key: key, Purpose: "导出报告", Summary: "已验证导出报告", Audience: []work.Actor{{Kind: work.ActorRole, ID: "leader"}}, Retention: work.RetentionRetained, References: []work.DeliveryRef{{ResourceID: f.resource.ID, Current: true}, {ResourceID: saved.ResourceID, VersionID: saved.VersionID}}}
		if err := cli("delivery.publish", req, &value); err != nil || value.ObjectID == "" {
			t.Fatalf("publish %+v: %v", value, err)
		}
		return value
	}
	firstDelivery := pub("delivery-v1", first)
	if err := os.WriteFile(path, newFile, 0600); err != nil {
		t.Fatal(err)
	}
	modify := workservice.SaveResourceFileRequest{Key: "draft-two", Path: "report.pdf", ResourceID: first.ResourceID, ExpectedRevision: 1}
	if err := cli("resource.file.save", modify, &second); err != nil || second.ResourceID != first.ResourceID || second.VersionID == first.VersionID || second.Revision != 2 {
		t.Fatalf("revision %+v: %v", second, err)
	}
	pub("delivery-v2", second)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := cli("resource.file.save", initial, &replay); err != nil || !replay.Replay || replay.VersionID != first.VersionID {
		t.Fatalf("replay with removed source %+v: %v", replay, err)
	}
	flush := func() {
		t.Helper()
		if count, err := a.teamRuntime.publisher.RunOnce(ctx, 100); err != nil || count == 0 {
			t.Fatalf("expected actual publication count=%d err=%v", count, err)
		}
	}
	flush()
	var originalView workstore.UIView
	pickerTx(t, a, func(tx *workstore.Tx) error {
		views, err := tx.ListUIViews("scope")
		if err != nil {
			return err
		}
		for _, view := range views {
			if view.TargetID == firstDelivery.ObjectID {
				originalView = view
				break
			}
		}
		if originalView.ExternalMessageID == "" {
			return fmt.Errorf("missing delivered Feishu message receipt")
		}
		return nil
	})
	acceptClick := func(principal string, from workstore.UIView, action, event string, values map[string]any) workstore.HumanInteraction {
		t.Helper()
		if values == nil {
			values = map[string]any{}
		}
		values["team_view"], values["action"] = from.ID, action
		var interaction workstore.HumanInteraction
		pickerTx(t, a, func(tx *workstore.Tx) error {
			var err error
			interaction, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: identity.GatewayRef, AppID: identity.AppID, EventID: event, Tenant: "tenant", ChatID: "chat", MessageID: from.ExternalMessageID, SenderIDs: work.SenderIDs{OpenID: principal}, Kind: "card", Values: values, ReceivedAt: time.Now().UTC()}, identity)
			return err
		})
		return interaction
	}
	clickAs := func(principal string, from workstore.UIView, action, event string, values map[string]any) workstore.HumanInteraction {
		t.Helper()
		interaction := acceptClick(principal, from, action, event, values)
		if err := a.processTeamInteraction(ctx, interaction); err != nil {
			t.Fatalf("%s: %v", event, err)
		}
		pickerTx(t, a, func(tx *workstore.Tx) error {
			var err error
			interaction, err = tx.GetHumanInteraction(interaction.ID)
			return err
		})
		return interaction
	}
	click := func(from workstore.UIView, action, event string, values map[string]any) workstore.HumanInteraction {
		return clickAs("owner", from, action, event, values)
	}
	viewByRef := func(id string) workstore.UIView {
		t.Helper()
		var result workstore.UIView
		pickerTx(t, a, func(tx *workstore.Tx) error {
			var err error
			result, err = tx.GetUIView(id)
			return err
		})
		return result
	}
	if err := a.runTeamProjectViews(ctx); err != nil {
		t.Fatal(err)
	}
	flush()
	var status workstore.UIView
	pickerTx(t, a, func(tx *workstore.Tx) error {
		views, err := tx.ListUIViews("scope")
		for _, view := range views {
			if view.Kind == "project.status" && view.ExternalMessageID != "" {
				status = view
			}
		}
		return err
	})
	index := click(status, "project.deliveries.open", "history", nil)
	flush()
	otherIndex := clickAs("second-member", status, "project.deliveries.open", "other-history", nil)
	flush()
	detail := click(viewByRef(index.ResultRef), "project.deliveries.inspect", "history-old", map[string]any{"delivery_id": firstDelivery.ObjectID})
	flush()
	otherDetail := clickAs("second-member", viewByRef(otherIndex.ResultRef), "project.deliveries.inspect", "other-history-old", map[string]any{"delivery_id": firstDelivery.ObjectID})
	flush()
	reader := click(viewByRef(detail.ResultRef), "artifacts.open", "open-v1", nil)
	flush()
	otherReader := clickAs("second-member", viewByRef(otherDetail.ResultRef), "artifacts.open", "other-open-v1", nil)
	flush()
	readerView := viewByRef(reader.ResultRef)
	if readerView.Invalidated || viewByRef(otherReader.ResultRef).Invalidated {
		t.Fatal("another group member's browser replaced the current reader")
	}
	click(readerView, "artifacts.download", "download-v1", map[string]any{"asset": 0})
	select {
	case got := <-uploads:
		if !bytes.Equal(got, oldFile) {
			t.Fatalf("downloaded bytes differ from historical file: %q", got)
		}
	default:
		t.Fatal("frozen file did not reach Feishu SDK")
	}
	select {
	case target := <-fileReplies:
		if target != "/open-apis/im/v1/messages/"+readerView.ExternalMessageID+"/reply" {
			t.Fatalf("file replied to %s", target)
		}
	default:
		t.Fatal("frozen file was uploaded but not sent")
	}
	assertDeliveryJourneyFeedback(t, a, identity, originalView.ExternalMessageID, first.VersionID, second.VersionID)

	holdUpload.Store(true)
	staleDownload := acceptClick("second-member", viewByRef(otherReader.ResultRef), "artifacts.download", "download-during-rebind", map[string]any{"asset": 0})
	downloadDone := make(chan error, 1)
	go func() { downloadDone <- a.processTeamInteraction(ctx, staleDownload) }()
	released := false
	defer func() {
		if !released {
			close(allowUpload)
		}
	}()
	select {
	case <-uploadStarted:
	case err := <-downloadDone:
		t.Fatalf("download failed before reaching upload barrier: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("download did not reach upload barrier")
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		old, err := tx.GetProjectChannelBinding("tenant", "chat")
		if err != nil {
			return err
		}
		old.Active = false
		old.BindingGeneration++
		if err = tx.PutProjectChannelBinding(old, old.BindingGeneration-1); err != nil {
			return err
		}
		return tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: old.ProjectID, Tenant: old.Tenant, ChatID: "replacement-chat", Purpose: "main", BindingGeneration: 1, Active: true}, 0)
	})
	ingress, err := workingress.New(a.teamRuntime.store, workingress.Config{Directory: deliveryJourneyDirectory{identity}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ingress.Receive(ctx, work.RawIngressEnvelope{GatewayRef: identity.GatewayRef, AppID: identity.AppID, EventID: "new-group-member", Tenant: "tenant", ChatID: "replacement-chat",
		ChatType: "group", MessageID: "new-group-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "second-member"},
		MessageType: "text", RawContent: `{"text":"我已进入新项目群。"}`, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := ingress.ProcessPending(ctx, 16); err != nil || outcome.Processed != 1 {
		t.Fatalf("new group access was not admitted: %+v %v", outcome, err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		return tx.RequireProjectPrincipalAccess("scope", staleDownload.PrincipalID, time.Now())
	})
	close(allowUpload)
	released = true
	select {
	case err := <-downloadDone:
		if err == nil || !strings.Contains(err.Error(), "project conversation binding changed") {
			t.Fatalf("old group download should be fenced after upload: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old download did not settle")
	}
	select {
	case reply := <-fileReplies:
		t.Fatalf("uploaded artifact escaped into old project group: %s", reply)
	default:
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		operations, err := tx.ListOperations("scope")
		if err != nil {
			return err
		}
		for _, op := range operations {
			if op.Kind == "feishu.file.publish" && op.Actor.ID == staleDownload.PrincipalID {
				if len(op.Steps) == 2 && op.Steps[0].State == work.OperationConfirmed && op.Steps[0].Receipt == "file_saved_version" && op.Steps[1].State == work.OperationPrepared {
					return nil
				}
				return fmt.Errorf("uploaded bytes were not retained with unsent receipt: %+v", op)
			}
		}
		return fmt.Errorf("upload operation is missing")
	})
}

func startDeliveryJourneyRun(t *testing.T, store *workstore.Store, dir, label, existingInput string, tasks ...work.Task) work.ExecutionAttempt {
	t.Helper()
	var attempt work.ExecutionAttempt
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		wc := work.WorkContext{ID: label + "-context", ScopeID: "scope", RoleID: "leader", Workstream: label, Revision: 1}
		var task work.Task
		if len(tasks) > 0 {
			task = tasks[0]
			wc.TaskID = task.ID
		}
		if err := tx.PutContext(wc, 0); err != nil {
			return err
		}
		location := work.ExecutionLocation{ID: label + "-location", HostID: "local-delivery-host", Cwd: dir, IsolatedContextID: wc.ID, Generation: 1, Revision: 1}
		if err := tx.PutLocation(location, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: label + "-worker", ContextID: wc.ID, ScopeID: "scope", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: label + "-instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationID: location.ID, LocationGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		if existingInput == "" {
			existingInput = label + "-input"
			if err := tx.PutInbox(work.Inbox{ID: existingInput, ScopeID: wc.ScopeID, RecipientRoleID: wc.RoleID, RecipientContextID: wc.ID, TaskID: wc.TaskID, EventID: existingInput, State: work.InboxPending, ContentState: work.InputContentReady, Revision: 1}, 0); err != nil {
				return err
			}
		}
		attempt = work.ExecutionAttempt{ID: label + "-attempt", ContextID: wc.ID, ScopeID: wc.ScopeID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: label + "-run"}, State: work.AttemptDispatchPending, InputIDs: []string{existingInput}, CommandID: label + "-start", Revision: 1}
		attempt.TaskID, attempt.FrozenRequirementsRevision, attempt.FrozenAssignmentGeneration = task.ID, task.RequirementsRevision, task.AssignmentGeneration
		op := work.Operation{ID: attempt.CommandID, ScopeID: wc.ScopeID, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "fixture", State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now()}); err != nil {
			return err
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	}); err != nil {
		t.Fatal(err)
	}
	return attempt
}

func assertDeliveryJourneyFeedback(t *testing.T, a *App, identity work.IngressGateway, messageID, oldVersion, newVersion string) {
	t.Helper()
	ctx := t.Context()
	store := a.teamRuntime.store
	ingress, err := workingress.New(store, workingress.Config{Directory: deliveryJourneyDirectory{identity}})
	if err != nil {
		t.Fatal(err)
	}
	envelope := work.RawIngressEnvelope{GatewayRef: identity.GatewayRef, AppID: identity.AppID, EventID: "feedback-event", Tenant: "tenant", ChatID: "chat", ChatType: "group", MessageID: "human-delivery-feedback", ParentID: messageID, RootID: messageID, SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "owner"}, MessageType: "text", RawContent: `{"text":"这份报告的提示太长了"}`, ReceivedAt: time.Now().UTC()}
	if _, err = ingress.Receive(ctx, envelope); err != nil {
		t.Fatal(err)
	}
	if _, err = ingress.ProcessPending(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var inputID string
	pickerTx(t, a, func(tx *workstore.Tx) error {
		items, err := tx.ListPendingInputs("scope", "leader")
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.MessageID == "" {
				continue
			}
			message, err := tx.GetMessage(item.MessageID)
			if err != nil && !errors.Is(err, workstore.ErrNotFound) {
				return err
			}
			if message.PlatformKey != nil && message.PlatformKey.MessageID == envelope.MessageID {
				inputID = item.ID
				return nil
			}
		}
		return fmt.Errorf("feedback was not admitted")
	})
	attempt := startDeliveryJourneyRun(t, store, t.TempDir(), "feedback", inputID)
	prompt, err := a.teamRuntime.service.BuildWorkInput(ctx, attempt)
	if err != nil || !strings.Contains(prompt, `"related_delivery": {`) || !strings.Contains(prompt, oldVersion) || strings.Contains(prompt, newVersion) || !strings.Contains(prompt, `"current": true`) || !strings.Contains(prompt, "这份报告的提示太长了") {
		t.Fatalf("feedback did not resolve the original delivered references: %v\n%s", err, prompt)
	}
}
