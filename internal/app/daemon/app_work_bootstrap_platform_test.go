package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workprovision"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type bootstrapJourneyPublisher struct {
	gateways map[string]*feishu.LiveGateway
}

func (p bootstrapJourneyPublisher) Resolve(_ context.Context, binding work.RoleBotBinding) (workpublish.Sender, error) {
	return p.gateways[binding.RoleID].ProjectChatAPI(), nil
}

func bootstrapJourneyExternalProvision(t *testing.T, a *App, receipt workservice.Result, source work.Message, locationID string) {
	t.Helper()
	r := a.teamRuntime
	chat := source.PlatformKey.ChatID
	platform := &bootstrapJourneyPlatform{t: t, chat: chat, document: "doc_" + chat, blocks: map[string]map[string]any{}}
	server := httptest.NewServer(platform)
	defer server.Close()
	gateways := map[string]*feishu.LiveGateway{}
	for _, role := range []string{"leader", "expert"} {
		var binding work.RoleBotBinding
		pickerTx(t, a, func(tx *workstore.Tx) error {
			var e error
			binding, e = tx.GetRoleBotBinding(role)
			return e
		})
		gateways[role] = feishu.NewLiveGateway(feishu.LiveGatewayConfig{GatewayID: binding.GatewayRef, AppID: binding.AppID, AppSecret: "fixture", Domain: server.URL})
		switch role {
		case "leader":
			platform.leaderAppID = binding.AppID
		case "expert":
			platform.expertAppID = binding.AppID
		}
	}
	var err error
	provision, err := workprovision.New(r.store, workprovision.Options{
		Resolver: workprovision.GatewayResolver(func(_ context.Context, binding work.RoleBotBinding) (*feishu.LiveGateway, error) {
			return gateways[binding.RoleID], nil
		}),
		Execution: workprovision.ExecutionPreparerFunc(func(_ context.Context, project work.Project, ids []string) error {
			expected := []string{}
			if locationID != "" {
				expected = append(expected, locationID)
			}
			if project.ID != receipt.ObjectID || !slices.Equal(ids, expected) {
				return fmt.Errorf("prepared unexpected project or location: %s %v", project.ID, ids)
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	r.provision = provision
	r.publisher, err = workpublish.New(r.store, bootstrapJourneyPublisher{gateways}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.runProvisioning(t.Context()); err != nil {
		t.Fatal(err)
	}
	var home workstore.ProjectHome
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var e error
		home, e = tx.GetProjectHome(receipt.ObjectID)
		return e
	})
	if err = a.advanceTeamProjectNavigation(t.Context(), home); err != nil {
		t.Fatal(err)
	}
	if _, err = r.publisher.RunOnce(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	if err = a.advanceTeamProjectNavigation(t.Context(), home); err != nil {
		t.Fatal(err)
	}
	if err = r.runProvisioning(t.Context()); err != nil { // replay must keep all identities stable
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		project, err := tx.GetProject(receipt.ObjectID)
		if err != nil {
			return err
		}
		currentHome, err := tx.GetProjectHome(project.ID)
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(receipt.OperationID)
		if err != nil {
			return err
		}
		if project.State != work.ProjectActive || currentHome.TabID == "" || currentHome.PinMessageID == "" || currentHome.DocumentID != platform.document {
			return fmt.Errorf("establishment incomplete: state=%s op=%s %q home=%+v", project.State, op.State, op.LastError, currentHome)
		}
		members, err := tx.ListProjectMembers(project.ID)
		if err != nil {
			return err
		}
		if len(members) != 2 || members[0].State != work.ProjectMemberReady || members[1].State != work.ProjectMemberReady {
			return fmt.Errorf("members not ready: %+v", members)
		}
		inputs, err := tx.ListPendingInputs(project.ScopeID, project.LeaderRoleID)
		if err != nil {
			return err
		}
		if len(inputs) != 1 {
			return fmt.Errorf("provisioning produced %d first work inputs", len(inputs))
		}
		message, err := tx.GetMessage(inputs[0].MessageID)
		if err != nil {
			return err
		}
		actual, err := tx.GetContent(project.ScopeID, message.BodyRef)
		if err != nil {
			return err
		}
		original, err := tx.GetContent(source.ScopeID, source.BodyRef)
		if err != nil {
			return err
		}
		if message.DerivedFrom == nil || message.DerivedFrom.ID != source.ID || actual.SHA256 != original.SHA256 {
			return fmt.Errorf("first work did not retain original group request")
		}
		return nil
	})
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if platform.invites != 1 || platform.creates != 1 || platform.tabs != 1 || platform.pins != 1 {
		t.Fatalf("unexpected side effects: invitations=%d documents=%d tabs=%d pins=%d", platform.invites, platform.creates, platform.tabs, platform.pins)
	}
}

type bootstrapJourneyPlatform struct {
	mu                                     sync.Mutex
	t                                      *testing.T
	chat, document                         string
	leaderAppID, expertAppID               string
	invites, creates, tabs, pins, messages int
	blocks                                 map[string]map[string]any
}

func (p *bootstrapJourneyPlatform) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if r.Method == "POST" || r.Method == "PATCH" {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			p.t.Errorf("decode %s: %v", r.URL.Path, err)
		}
	}
	path := r.URL.Path
	var data any = map[string]any{}
	switch {
	case path == larkcore.TenantAccessTokenInternalUrlPath:
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "tenant_access_token": "token-" + fmt.Sprint(body["app_id"]), "expire": 7200})
		return
	case strings.HasSuffix(path, "/members/is_in_chat"):
		data = map[string]any{"is_in_chat": r.Header.Get("Authorization") == "Bearer token-"+p.leaderAppID || p.invites != 0}
	case strings.HasSuffix(path, "/members") && strings.Contains(path, "/im/v1/chats/") && r.Method == "POST":
		p.invites++
		if r.Header.Get("Authorization") != "Bearer token-"+p.leaderAppID || r.URL.Query().Get("member_id_type") != "app_id" || fmt.Sprint(body["id_list"]) != "["+p.expertAppID+"]" {
			p.t.Errorf("invitation used wrong App identity or member: %s %#v", r.URL, body)
		}
	case strings.HasSuffix(path, "/files/create_folder"):
		data = map[string]any{"token": "folder-" + p.chat}
	case strings.HasSuffix(path, "/docx/v1/documents"):
		p.creates++
		data = p.documentData()
	case strings.HasSuffix(path, "/metas/batch_query"):
		data = map[string]any{"metas": []any{map[string]any{"doc_token": p.document, "url": "https://local.feishu.cn/docx/" + p.document}}}
	case strings.Contains(path, "/permissions/") && strings.HasSuffix(path, "/members"):
		data = map[string]any{"members": []any{map[string]any{"member_type": "openchat", "member_id": p.chat, "perm": "view"}}}
	case strings.HasSuffix(path, "/children") && r.Method == "POST":
		children, _ := body["children"].([]any)
		for i, raw := range children {
			child := raw.(map[string]any)
			id := fmt.Sprintf("block-%d", i)
			child["block_id"] = id
			p.blocks[id] = child
		}
		data = map[string]any{"children": children, "document_revision_id": 1}
	case strings.Contains(path, "/blocks/") && r.Method == "GET":
		data = map[string]any{"block": p.blocks[path[strings.LastIndex(path, "/")+1:]]}
	case strings.HasSuffix(path, "/"+p.document):
		data = p.documentData()
	case strings.HasSuffix(path, "/batch_update") && strings.Contains(path, "/blocks/"):
		data = map[string]any{"document_revision_id": 2}
	case strings.HasSuffix(path, "/chat_tabs") && r.Method == "POST":
		p.tabs++
		tabs := body["chat_tabs"].([]any)
		tabs[0].(map[string]any)["tab_id"] = "tab-project"
		data = map[string]any{"chat_tabs": tabs}
	case path == "/open-apis/im/v1/messages" || strings.HasSuffix(path, "/reply"):
		p.messages++
		data = map[string]any{"message_id": fmt.Sprintf("om_%s_%d", p.chat, p.messages), "chat_id": p.chat}
	case path == "/open-apis/im/v1/pins" && r.Method == "POST":
		p.pins++
		data = map[string]any{"pin": map[string]any{"message_id": body["message_id"], "chat_id": p.chat}}
	default:
		p.t.Errorf("unexpected controlled Feishu request %s %s", r.Method, r.URL)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func (p *bootstrapJourneyPlatform) documentData() map[string]any {
	return map[string]any{"document": map[string]any{"document_id": p.document, "revision_id": 1, "title": "项目首页"}}
}
