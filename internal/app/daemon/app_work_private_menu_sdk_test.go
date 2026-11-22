package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func TestPrivateMenuWholeJourneyThroughFeishuSDK(t *testing.T) {
	const gatewayID, chat, messageID = "sdk-menu", "oc_private_menu", "om_private_menu"
	const model = "menu-fixture-model"
	appID := isolatedSDKFixtureAppID(t, "private-menu")
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: gatewayID, AppID: appID, AppSecret: "isolated"}}
	profile, err := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{Name: "菜单旅程", BaseURL: "https://example.invalid/v1", APIKey: "unused", Model: model, ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{profile}
	controller := feishu.NewMultiGatewayController()
	a, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), controller, false, "")
	setTestWorkBotSettings(t, a, state.BotCapabilitySettingsRecord{GatewayID: gatewayID, Backend: agentproto.BackendCodex, CodexProfileID: profile.ID})
	store, err := workstore.Open(t.Context(), filepath.Join(t.TempDir(), "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	runtime := &teamRuntime{app: a, store: store}
	a.teamRuntime = runtime
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.service, err = workservice.New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	directory := teamDirectory{app: a, store: store}
	runtime.ingress, err = workingress.New(store, workingress.Config{Directory: directory, PrivateCommands: teamPrivateCommands{app: a}})
	if err != nil {
		t.Fatal(err)
	}
	runtime.publisher, err = workpublish.New(store, teamPublisherResolver{directory: directory}, nil)
	if err != nil {
		t.Fatal(err)
	}
	roleID := teamID("role", gatewayID)
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: roleID, Name: "助手", Description: "协助工作", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: roleID, GatewayRef: gatewayID, AppID: appID, IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		return tx.PutLocation(work.ExecutionLocation{ID: "fixture-dir", HostID: teamID("host", hostname+":"+a.headlessRuntime.Paths.StateDir), Cwd: dir, Generation: 1, Revision: 1}, 0)
	})
	runtime.active.Store(true)

	connected := make(chan *websocket.Conn, 1)
	cards := make(chan string, 20)
	var creates, updates, entrySends atomic.Int32
	var losePatch atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case larkws.GenEndpointUri:
			_ = json.NewEncoder(w).Encode(larkws.EndpointResp{Code: 0, Data: &larkws.Endpoint{Url: "ws://" + req.Host + "/ws?device_id=menu&service_id=1"}})
			return
		case "/ws":
			upgrader := websocket.Upgrader{}
			ws, e := upgrader.Upgrade(w, req, nil)
			if e != nil {
				t.Error(e)
				return
			}
			connected <- ws
			return
		case larkcore.TenantAccessTokenInternalUrlPath:
			_, _ = fmt.Fprintf(w, `{"code":0,"tenant_access_token":%q,"expire":7200}`, "token-"+appID)
			return
		}
		var body struct {
			Kind          string `json:"msg_type"`
			Content       string `json:"content"`
			ReplyInThread bool   `json:"reply_in_thread"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || req.Header.Get("Authorization") != "Bearer token-"+appID || body.ReplyInThread {
			t.Errorf("invalid platform request %s %s: %+v %v", req.Method, req.URL, body, err)
		}
		var id string
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/open-apis/im/v1/messages" && body.Kind == "text":
			entrySends.Add(1)
			id = "om_entry"
		case req.Method == http.MethodPost && (req.URL.Path == "/open-apis/im/v1/messages/om_entry/reply" || req.URL.Path == "/open-apis/im/v1/messages") && body.Kind == "interactive":
			if creates.Add(1) == 1 {
				id = messageID
			} else {
				id = "om_new_menu"
			}
			cards <- body.Content
		case req.Method == http.MethodPatch && req.URL.Path == "/open-apis/im/v1/messages/"+messageID:
			updates.Add(1)
			if losePatch.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprint(w, `{"code":1,"msg":"patch outcome unknown"}`)
				return
			}
			id = messageID
			cards <- body.Content
		default:
			t.Errorf("unexpected platform request %s %s", req.Method, req.URL)
			http.NotFound(w, req)
			return
		}
		_, _ = fmt.Fprintf(w, `{"code":0,"data":{"message_id":%q,"chat_id":%q}}`, id, chat)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err = controller.UpsertApp(ctx, feishu.GatewayAppConfig{GatewayID: gatewayID, AppID: appID, AppSecret: "isolated", Domain: server.URL, Enabled: true, BotOpenID: "ou_bot", RawIngressHook: a.teamIngressHook(), PreviewCacheDir: t.TempDir(), PreviewStatePath: filepath.Join(t.TempDir(), "preview.json")}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- controller.Start(ctx, nil) }()
	defer func() {
		cancel()
		if e := <-done; e != nil {
			t.Error(e)
		}
	}()
	var ws *websocket.Conn
	select {
	case ws = <-connected:
		defer ws.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("SDK WebSocket did not connect")
	}
	event := func(id, kind string, data map[string]any) string {
		return string(bootstrapSDKEvent(t, ws, id, map[string]string{"event_id": id, "event_type": kind, "app_id": appID, "tenant_key": "tenant"}, data).Data)
	}
	openMenu := func(id string) {
		t.Helper()
		event(id, "application.bot.menu_v6", map[string]any{"event_key": "menu", "operator": map[string]any{"operator_id": map[string]string{"open_id": "ou_human"}}})
		if e := a.runTeamMenus(ctx); e != nil {
			t.Fatal(e)
		}
	}
	publish := func() string {
		t.Helper()
		if done, e := runtime.publisher.RunOnce(ctx, 16); e != nil || done != 1 {
			t.Fatalf("published %d: %v", done, e)
		}
		select {
		case card := <-cards:
			return card
		default:
			t.Fatal("no real HTTP card request")
			return ""
		}
	}
	click := func(id, message string, value, form map[string]any) string {
		return event(id, "card.action.trigger", map[string]any{"operator": map[string]string{"open_id": "ou_human", "tenant_key": "tenant"}, "context": map[string]string{"open_chat_id": chat, "open_message_id": message}, "action": map[string]any{"value": value, "form_value": form}})
	}
	follow := func(card, id, action, argument string, form map[string]any) string {
		t.Helper()
		value := menuSDKAction(t, card, action, argument)
		if ack := click(id, messageID, value, form); !strings.Contains(ack, "已收到") {
			t.Fatalf("%s callback ack %s", id, ack)
		}
		if e := a.processTeamInteractions(ctx); e != nil {
			t.Fatal(e)
		}
		return publish()
	}

	openMenu("menu")
	readFront := func() work.PrivateNavigation {
		t.Helper()
		var target work.PrivateNavigation
		pickerTx(t, a, func(tx *workstore.Tx) error {
			scopes, e := tx.ListScopes()
			if e != nil {
				return e
			}
			for _, scope := range scopes {
				if scope.Kind == work.ScopePersonal {
					target, e = tx.GetPrivateNavigation(scope.ID)
					return e
				}
			}
			return workstore.ErrNotFound
		})
		return target
	}
	original := readFront()
	home := publish()
	for _, group := range []string{"基本命令", "参数设置", "工作区与会话", "常用工具", "系统管理"} {
		if !strings.Contains(home, group) {
			t.Fatalf("menu missing %s", group)
		}
	}
	settings := follow(home, "settings", "private.settings", "", nil)
	selection := follow(settings, "model", "settings.page", "model", nil)
	if !strings.Contains(selection, model) || !strings.Contains(selection, `"initial_option":"clear"`) {
		t.Fatalf("frozen fixed model and current choice missing: %s", selection)
	}
	saved := follow(selection, "save", "settings.apply", "", map[string]any{"selection": model})
	if !strings.Contains(saved, "已保存") || !strings.Contains(saved, model) {
		t.Fatal("model save did not update its own card")
	}
	settings = follow(saved, "settings-back", "settings.page", "overview", nil)
	home = follow(settings, "home", "private.menu", "", nil)
	workspace := follow(home, "workspace", "private.workspace", "", nil)
	location := follow(workspace, "dirs", "private.resources", "", nil)
	chosen := follow(location, "choose-dir", "picker.location.select", "", nil)
	if !strings.Contains(chosen, "已将所选目录设为默认") || !strings.Contains(chosen, dir) {
		t.Fatal("selection didn't return persisted directory on the original card")
	}
	workspace = follow(chosen, "workspace-back", "private.workspace", "", nil)
	creation := follow(workspace, "workspace-creation", "private.workspace-tools", "", nil)
	status := follow(creation, "submit-workspace", "private.workspace-dir", "", map[string]any{"workspace_dir": "新的目录"})
	if !strings.Contains(status, "等待助手处理") {
		t.Fatal("the actual workspace form did not queue the requested work")
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		inputs, e := tx.ListPendingInputs(original.PersonalScopeID, roleID)
		if e != nil || len(inputs) != 1 || inputs[0].RecipientContextID != original.FrontContextID {
			t.Fatalf("workspace form was not assigned to the original private context: %+v %v", inputs, e)
		}
		typed, e := tx.GetMessage(inputs[0].MessageID)
		if e != nil {
			return e
		}
		body, e := tx.GetContent(original.PersonalScopeID, typed.BodyRef)
		if e == nil && (string(body.Data) != "/workspace new dir 新的目录" || typed.DerivedFrom == nil || typed.DerivedFrom.Kind != "human_interaction") {
			t.Fatalf("queued workspace request lost actual form authority: %+v %s", typed, body.Data)
		}
		return e
	})
	basic := follow(status, "status-back", "private.basic", "", nil)
	home = follow(basic, "basic-back", "private.menu", "", nil)
	workspace = follow(home, "workspace-again", "private.workspace", "", nil)
	workspace = follow(workspace, "new-context", "private.new", "", nil)
	newer := readFront()
	if newer.FrontContextID == original.FrontContextID {
		t.Fatal("new session did not change subsequent private input ownership")
	}
	history := follow(workspace, "history", "private.contexts", "", nil)
	view := follow(history, "read", "history.read", original.FrontContextID, nil)
	if !strings.Contains(view, "新的目录") || readFront().FrontContextID != newer.FrontContextID {
		t.Fatal("history must show the old discussion without retargeting new private input")
	}
	history = follow(view, "history-back", "history.contexts-page", "", nil)
	history = follow(history, "switch", "history.select-context", original.FrontContextID, nil)
	if !strings.Contains(history, "已切换会话") || readFront().FrontContextID != original.FrontContextID {
		t.Fatal("explicit switch did not commit a work context")
	}
	workspace = follow(history, "history-workspace-back", "private.workspace", "", nil)
	final := follow(workspace, "final-home", "private.menu", "", nil)
	if creates.Load() != 1 || updates.Load() != 21 || entrySends.Load() != 1 {
		t.Fatalf("one menu card and twenty-one SDK patches required, got %d creates + %d patches + %d entries", creates.Load(), updates.Load(), entrySends.Load())
	}
	if ack := click("stale", messageID, menuSDKAction(t, home, "private.workspace", ""), nil); !strings.Contains(ack, "已过期") {
		t.Fatalf("earlier view could act again: %s", ack)
	}
	if ack := click("forwarded", "om_forwarded", menuSDKAction(t, final, "private.workspace", ""), nil); !strings.Contains(ack, "已过期") {
		t.Fatalf("forwarded card could act: %s", ack)
	}
	losePatch.Store(true)
	if ack := click("lost-patch", messageID, menuSDKAction(t, final, "private.workspace", ""), nil); !strings.Contains(ack, "已收到") {
		t.Fatalf("lost patch before requesting update: %s", ack)
	}
	if err = a.processTeamInteractions(ctx); err != nil {
		t.Fatal(err)
	}
	_, _ = runtime.publisher.RunOnce(ctx, 16)
	_, _ = runtime.publisher.RunOnce(ctx, 16)
	if updates.Load() != 22 || creates.Load() != 1 {
		t.Fatalf("unknown PATCH retried or appended: %d/%d", updates.Load(), creates.Load())
	}
	openMenu("refresh-menu")
	if fresh := publish(); !strings.Contains(fresh, "工作菜单") || creates.Load() != 2 {
		t.Fatalf("fresh platform menu didn't recover: %s", fresh)
	}
}

func menuSDKAction(t *testing.T, card, action, setting string) map[string]any {
	t.Helper()
	var raw struct {
		Body struct {
			Elements []teamCardElement `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(card), &raw); err != nil {
		t.Fatal(err)
	}
	argumentKey := "setting"
	if strings.HasPrefix(action, "history.") {
		argumentKey = "context_id"
	}
	var find func([]teamCardElement) map[string]any
	find = func(elements []teamCardElement) map[string]any {
		for _, e := range elements {
			for _, behavior := range e.Behaviors {
				if behavior.Value["action"] == action && (setting == "" || behavior.Value[argumentKey] == setting) {
					return behavior.Value
				}
			}
			if v := find(e.Elements); v != nil {
				return v
			}
		}
		return nil
	}
	value := find(raw.Body.Elements)
	if value == nil {
		t.Fatalf("action %s/%s missing in %s", action, setting, card)
	}
	return value
}
