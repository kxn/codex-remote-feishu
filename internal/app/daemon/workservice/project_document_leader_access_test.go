package workservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestProjectLeaderRestoresExpertDocumentUsingOriginalApp(t *testing.T) {
	const sourceID = "restore-expert-document"
	const creatorApp = "project-document-acl-expert-app"
	const permissionPath = "/open-apis/drive/v1/permissions/specialist-original/members"
	s, leaderToken, leaderRun := lifecycleProjectService(t, false, sourceID)
	ctx := t.Context()
	claimTime := s.now()
	s.now = func() time.Time { return claimTime.Add(-time.Second) }
	var readCount, grants atomic.Int64
	var shared atomic.Bool
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			var req struct {
				AppID string `json:"app_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AppID != creatorApp {
				t.Errorf("unexpected application token request %s: %v", req.AppID, err)
			}
			fmt.Fprintf(w, `{"code":0,"tenant_access_token":%q,"expire":7200}`, creatorApp)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+creatorApp {
			t.Error("document access was executed using a different application")
		}
		switch {
		case r.URL.Path == "/open-apis/docx/v1/documents" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"specialist-original","revision_id":1}}}`)
		case r.URL.Path == permissionPath && r.Method == http.MethodGet:
			readCount.Add(1)
			if shared.Load() {
				fmt.Fprint(w, `{"code":0,"data":{"items":[{"member_id":"group","member_type":"openchat","type":"chat","perm":"view"}]}}`)
			} else {
				fmt.Fprint(w, `{"code":0,"data":{"items":[]}}`)
			}
		case r.URL.Path == permissionPath && r.Method == http.MethodPost:
			var grant struct {
				MemberID   string `json:"member_id"`
				MemberType string `json:"member_type"`
				Permission string `json:"perm"`
			}
			if err := json.NewDecoder(r.Body).Decode(&grant); err != nil || grant.MemberID != "group" || grant.MemberType != "openchat" || grant.Permission != "view" {
				t.Errorf("grant target changed: %+v, %v", grant, err)
			}
			grants.Add(1)
			shared.Store(true)
			fmt.Fprint(w, `{"code":0,"data":{}}`)
		default:
			t.Errorf("unexpected platform request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(platform.Close)
	expert := work.RoleBotBinding{RoleID: "expert", GatewayRef: "expert-gateway", AppID: "expert-app", IdentityGeneration: 1}
	member := work.RoleBotBinding{RoleID: "member", GatewayRef: "member-gateway", AppID: "member-app", IdentityGeneration: 1}
	connector, err := NewFeishuResourceConnector(FeishuConnectorIdentity(expert), feishu.NewLiveGateway(feishu.LiveGatewayConfig{GatewayID: expert.GatewayRef, AppID: creatorApp, AppSecret: "fixture-only", Domain: platform.URL}))
	if err != nil {
		t.Fatal(err)
	}
	s.connectors = map[string]ResourceConnector{"expert": connector}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutRole(work.Role{ID: member.RoleID, Name: "Member", Description: "Contribute", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(member, 0); err != nil {
			return err
		}
		if err := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: member.RoleID, Active: true, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: member.RoleID, State: work.ProjectMemberReady, BotGeneration: 1, Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutResource(work.Resource{ID: "expert-parent", ScopeID: "project-scope", ConnectorIdentity: connector.Identity(), SourceIdentity: "folder:project-parent", CanonicalURI: "https://a.feishu.cn/drive/folder/project-parent", CollectionID: "materials", Purpose: "Folder", Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	expertToken, expertRun := startCrossRoleResourceRun(t, s, "project-scope", expert, "creator-application-run")
	memberToken, memberRun := startCrossRoleResourceRun(t, s, "project-scope", member, "project-member-run")
	created, err := s.ResourceCreateDocument(ctx, expertToken, expertRun, CreateResourceDocumentRequest{Key: "expert-original", ParentResourceID: "expert-parent", ExpectedParentRevision: 1, Title: "Specialist original", Purpose: "Specialist original", CollectionID: "materials"})
	if err != nil || created.Status != "created" {
		t.Fatalf("expert creation = %+v, %v", created, err)
	}
	share := ShareProjectDocumentRequest{ResourceID: created.ObjectID}
	if _, err := s.ShareProjectDocument(ctx, memberToken, memberRun, share); !errors.Is(err, workstore.ErrScopeDenied) || readCount.Load() != 0 || grants.Load() != 0 {
		t.Fatalf("ordinary project member reached restricted access API: %v", err)
	}
	initial, err := s.ShareProjectDocument(ctx, leaderToken, leaderRun, share)
	if err != nil || initial.Status != "project_access_confirmed" || grants.Load() != 1 {
		t.Fatalf("leader initial grant via original application = %+v, %v", initial, err)
	}
	s.now = func() time.Time { return claimTime }
	shared.Store(false)
	if _, err := s.ShareProjectDocument(ctx, leaderToken, leaderRun, share); err != nil || grants.Load() != 1 {
		t.Fatalf("unsourced leader call after revocation sent another grant: %v", err)
	}
	share.SourceMessageID = sourceID
	restored, err := s.ShareProjectDocument(ctx, leaderToken, leaderRun, share)
	if err != nil || restored.Status != "project_access_confirmed" || restored.OperationID == initial.OperationID || grants.Load() != 2 {
		t.Fatalf("leader restoration using admitted human source = %+v, %v, grants=%d", restored, err, grants.Load())
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(restored.OperationID)
		if err != nil {
			return err
		}
		request, err := tx.GetContent("project-scope", op.RequestRef)
		if err != nil {
			return err
		}
		var frozen projectDocumentShare
		if err := json.Unmarshal(request.Data, &frozen); err != nil {
			return err
		}
		if op.Actor != (work.Actor{Kind: work.ActorRole, ID: "leader"}) || frozen.Binding != expert || frozen.RestoreSource == nil || frozen.RestoreSource.ID != sourceID {
			return fmt.Errorf("requesting role, original application or human provenance was lost: %+v, %+v", op.Actor, frozen)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
