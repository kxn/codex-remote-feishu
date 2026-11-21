package workservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func TestProjectFeishuResourceUsesEachRolesOwnBotAndFencesUnknownWriteRecovery(t *testing.T) {
	s, leaderToken, leaderRun := lifecycleProjectService(t, false)
	leader := work.RoleBotBinding{RoleID: "leader", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}
	expert := work.RoleBotBinding{RoleID: "expert", GatewayRef: "expert-gateway", AppID: "expert-app", IdentityGeneration: 1}
	rebound := work.RoleBotBinding{RoleID: "expert", GatewayRef: expert.GatewayRef, AppID: "replacement-app", IdentityGeneration: 2}
	const doc = "/open-apis/docx/v1/documents/shared-doc"
	var leaderCalls, expertCalls, replacementCalls, writes atomic.Int64
	var denied atomic.Bool
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == larkcore.TenantAccessTokenInternalUrlPath {
			var input struct {
				AppID string `json:"app_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Errorf("token request: %v", err)
			}
			fmt.Fprintf(w, `{"code":0,"tenant_access_token":%q,"expire":7200}`, input.AppID)
			return
		}
		switch strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") {
		case "collab-resource-leader":
			leaderCalls.Add(1)
		case "collab-resource-expert":
			expertCalls.Add(1)
			if denied.Load() {
				fmt.Fprint(w, `{"code":1770032,"msg":"expert document permission denied"}`)
				return
			}
		case "collab-resource-replacement":
			replacementCalls.Add(1)
		default:
			t.Errorf("unexpected SDK authorization %s", r.Header.Get("Authorization"))
		}
		switch {
		case r.URL.Path == doc:
			fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"shared-doc","revision_id":3}}}`)
		case r.URL.Path == doc+"/blocks/shared-doc":
			fmt.Fprint(w, `{"code":0,"data":{"block":{"block_id":"shared-doc","block_type":1}}}`)
		case r.URL.Path == doc+"/blocks/shared-line":
			fmt.Fprint(w, `{"code":0,"data":{"block":{"block_id":"shared-line","block_type":2,"text":{"elements":[{"text_run":{"content":"Unedited"}}]}}}}`)
		case r.URL.Path == doc+"/blocks":
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"block_id":"shared-line","block_type":2}],"has_more":false}}`)
		case r.URL.Path == doc+"/blocks/shared-doc/children" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") != "Bearer collab-resource-expert" {
				t.Error("cross-role write borrowed another Bot")
			}
			writes.Add(1)
			fmt.Fprint(w, `{"code":0,"data":{"document_revision_id":4,"children":[{"block_id":"specialist-result","block_type":2}]}}`)
		case r.URL.Path == doc+"/blocks/batch_update" && r.Method == http.MethodPatch:
			if r.Header.Get("Authorization") != "Bearer collab-resource-expert" {
				t.Error("uncertain write borrowed another Bot")
			}
			writes.Add(1)
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, `{"code":99,"msg":"write response lost"}`)
		case r.URL.Path == "/open-apis/drive/v1/files":
			fmt.Fprint(w, `{"code":0,"data":{"files":[],"has_more":false}}`)
		case r.URL.Path == "/open-apis/docx/v1/documents" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") != "Bearer collab-resource-expert" {
				t.Error("document creation borrowed the folder's registering Bot")
			}
			fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"specialist-created","revision_id":1}}}`)
		default:
			t.Errorf("unhandled SDK call %s %s", r.Method, r.URL)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer platform.Close()
	s.connectors = map[string]ResourceConnector{
		"leader": crossRoleFeishuConnector(platform, leader, "collab-resource-leader"),
		"expert": crossRoleFeishuConnector(platform, expert, "collab-resource-expert"),
	}
	expertToken, expertRun := startCrossRoleResourceRun(t, s, "project-scope", expert, "expert-first")
	register := RegisterResourceRequest{Key: "leader-doc", CollectionID: "project", SourceURL: "https://a.feishu.cn/docx/shared-doc", Purpose: "Source requirements"}
	legacyID := stableID("resource", "project-scope", FeishuConnectorIdentity(leader), "docx:shared-doc")
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutResource(work.Resource{ID: legacyID, ScopeID: "project-scope", ConnectorIdentity: FeishuConnectorIdentity(leader),
			SourceIdentity: "docx:shared-doc", CanonicalURI: register.SourceURL, Purpose: register.Purpose, CollectionID: register.CollectionID, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	first, err := s.ResourceRegister(t.Context(), leaderToken, leaderRun, register)
	if err != nil || first.ObjectID != legacyID {
		t.Fatalf("leader registration did not preserve historical identity: %+v %v", first, err)
	}
	leaderCount := leaderCalls.Load()
	register.Key, register.CollectionID = "expert-doc", "review"
	reused, err := s.ResourceRegister(t.Context(), expertToken, expertRun, register)
	if err != nil || reused.ObjectID != first.ObjectID || leaderCalls.Load() != leaderCount || expertCalls.Load() == 0 {
		t.Fatalf("specialist registration did not reuse the logical original with specialist permissions: %+v %+v %v", first, reused, err)
	}
	inspect := InspectResourceDocumentRequest{ResourceID: first.ObjectID, BlockID: "shared-doc"}
	if _, err = s.ResourceInspectDocument(t.Context(), expertToken, expertRun, inspect); err != nil {
		t.Fatal(err)
	}
	capture, err := s.ResourceCapture(t.Context(), expertToken, expertRun, CaptureResourceRequest{Key: "expert-capture", ResourceID: first.ObjectID})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.ResourceReadVersion(t.Context(), leaderToken, leaderRun, ReadResourceVersionRequest{ResourceID: first.ObjectID, VersionID: capture.ObjectID})
	if err != nil || saved.Version.ResourceID != first.ObjectID || saved.Content == nil {
		t.Fatalf("recorded snapshot has a different source: %+v %v", saved, err)
	}
	denied.Store(true)
	if _, err = s.ResourceInspectDocument(t.Context(), expertToken, expertRun, inspect); err == nil || !strings.Contains(err.Error(), "expert document permission denied") {
		t.Fatalf("native permission failure was hidden or fell back to leader: %v", err)
	}
	denied.Store(false)
	if leaderCalls.Load() != leaderCount {
		t.Fatal("specialist read used the leader's token")
	}
	appendRequest := AppendResourceDocumentRequest{Key: "expert-append", ResourceID: first.ObjectID, ExpectedResourceRevision: 1,
		ObservedDocumentRevision: 3, ParentBlockID: "shared-doc", Paragraphs: []string{"Reviewed"}}
	if result, err := s.ResourceAppendDocument(t.Context(), expertToken, expertRun, appendRequest); err != nil || result.Status != "written" || result.ObjectID != first.ObjectID {
		t.Fatalf("specialist append = %+v %v", result, err)
	}
	target, err := s.ResourceInspectDocument(t.Context(), expertToken, expertRun, InspectResourceDocumentRequest{ResourceID: first.ObjectID, BlockID: "shared-line"})
	if err != nil || len(target.Blocks) != 1 {
		t.Fatalf("inspect the exact target for the real update command: %+v %v", target, err)
	}
	edit := UpdateResourceDocumentRequest{Key: "expert-uncertain-update", ResourceID: first.ObjectID, ExpectedResourceRevision: 1,
		ObservedDocumentRevision: 3, Edits: []ResourceTextRunEdit{{BlockID: "shared-line", TextRunIndex: 0, ExpectedBlockSHA256: target.Blocks[0].SHA256, Text: "Edited"}}}
	payload, err := json.Marshal(edit)
	if err != nil {
		t.Fatal(err)
	}
	response, err := s.Execute(t.Context(), expertToken, workcmd.DispatchRequest{Name: "resource.document.update", RunRef: expertRun, Payload: payload})
	unknown, ok := response.(ResourceWriteResult)
	if err != nil || !ok || unknown.Status != "pending_external" {
		t.Fatalf("uncertain specialist write = %+v %v", response, err)
	}
	read := ObserveResourceWriteRequest{Key: "observe-as-actor", OperationID: unknown.OperationID}
	if _, err = s.ResourceObserveWrite(t.Context(), leaderToken, leaderRun, read); !errors.Is(err, workstore.ErrScopeDenied) {
		t.Fatalf("leader substituted their identity for the original expert: %v", err)
	}
	if observation, err := s.ResourceObserveWrite(t.Context(), expertToken, expertRun, read); err != nil || observation.Document == nil || observation.OriginalState != work.OperationUnknown {
		t.Fatalf("original expert could not perform safe readback: %+v %v", observation, err)
	}
	folder, err := s.ResourceRegister(t.Context(), leaderToken, leaderRun, RegisterResourceRequest{Key: "shared-folder", CollectionID: "project",
		SourceURL: "https://a.feishu.cn/drive/folder/shared-folder", Purpose: "Shared Drive folder", AsDirectory: true})
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.ResourceCreateDocument(t.Context(), expertToken, expertRun, CreateResourceDocumentRequest{Key: "specialist-doc",
		ParentResourceID: folder.ObjectID, ExpectedParentRevision: 1, Title: "Result", Purpose: "Review summary", CollectionID: "project"})
	if err != nil || created.Status != "created" {
		t.Fatalf("specialist creation = %+v %v", created, err)
	}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		resource, err := tx.GetResource("project-scope", created.ObjectID)
		if err == nil && resource.ConnectorIdentity != FeishuConnectorIdentity(expert) {
			return fmt.Errorf("document inherited folder provenance: %+v", resource)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	foreignToken, foreignRun := startCrossRoleResourceRun(t, s, "scope", leader, "personal-resource-lookup")
	leaderCount = leaderCalls.Load()
	if _, err = s.ResourceInspectDocument(t.Context(), foreignToken, foreignRun, inspect); err == nil || leaderCalls.Load() != leaderCount {
		t.Fatalf("cross-scope inspection invoked platform: %v", err)
	}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutRoleBotBinding(rebound, expert.IdentityGeneration)
	}); err != nil {
		t.Fatal(err)
	}
	s.connectors["expert"] = crossRoleFeishuConnector(platform, rebound, "collab-resource-replacement")
	newToken, newRun := startCrossRoleResourceRun(t, s, "project-scope", rebound, "expert-new-app")
	read.Key = "rebound-app-must-not-recover"
	if _, err = s.ResourceObserveWrite(t.Context(), newToken, newRun, read); !errors.Is(err, workstore.ErrScopeDenied) || replacementCalls.Load() != 0 || writes.Load() != 2 {
		t.Fatalf("new identity performed a read or retried an unknown write: %v", err)
	}
}

func crossRoleFeishuConnector(platform *httptest.Server, binding work.RoleBotBinding, appID string) *FeishuResourceConnector {
	client := lark.NewClient(appID, "fixture-only", lark.WithOpenBaseUrl(platform.URL), lark.WithHttpClient(platform.Client()))
	broker := feishu.NewFeishuCallBroker(binding.RoleID, client)
	return &FeishuResourceConnector{identity: FeishuConnectorIdentity(binding), documents: feishu.NewProjectDocumentAPI(broker), sheets: feishu.NewProjectSheetsAPI(broker)}
}

func startCrossRoleResourceRun(t *testing.T, s *Service, scope string, binding work.RoleBotBinding, id string) (string, string) {
	t.Helper()
	err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		wc := work.WorkContext{ID: id + "-context", ScopeID: scope, RoleID: binding.RoleID, Workstream: id, Revision: 1}
		if err := tx.PutContext(wc, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: id + "-worker", ContextID: wc.ID, ScopeID: scope, RoleID: binding.RoleID,
			Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic,
			ControllerGeneration: 1, InstanceID: id + "-instance", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		attempt := work.ExecutionAttempt{ID: id + "-attempt", ScopeID: scope, ContextID: wc.ID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: id},
			State: work.AttemptDispatchPending, CommandID: id + "-command", Revision: 1}
		op := work.Operation{ID: attempt.CommandID, ScopeID: scope, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"},
			Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: id, RequestHash: id, State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1,
			InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: op, Now: s.now()}); err != nil {
			return err
		}
		attempt.State, attempt.Revision = work.AttemptRunning, 2
		return tx.PutAttempt(attempt, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: binding.RoleID, BotGeneration: binding.IdentityGeneration,
		ScopeID: scope, ContextID: id + "-context", WorkerID: id + "-worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return token, id
}
