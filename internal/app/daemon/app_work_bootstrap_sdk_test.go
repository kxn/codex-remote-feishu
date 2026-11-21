package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func TestBootstrapLeaderChoiceThroughFeishuSDK(t *testing.T) {
	for _, scenario := range []struct {
		name                     string
		loseReply, differentCard bool
	}{
		{name: "confirmed_send"},
		{name: "unknown_send_confirmed_by_sdk_read", loseReply: true},
		{name: "unknown_send_with_different_card", loseReply: true, differentCard: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store, err := workstore.Open(t.Context(), filepath.Join(t.TempDir(), "work.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			a := &App{teamRuntime: &teamRuntime{store: store}}
			a.teamRuntime.app = a
			const gatewayID = "journey"
			appID := isolatedSDKFixtureAppID(t, "bootstrap-sdk")
			roleID := teamID("role", gatewayID)
			a.admin.loadConfig = func() (config.LoadedAppConfig, error) {
				return config.LoadedAppConfig{Config: config.AppConfig{Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{{ID: gatewayID, AppID: appID, AppSecret: "isolated"}}}}}, nil
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				if err := tx.PutRole(work.Role{ID: roleID, Name: "负责人", Description: "协调项目", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
					return err
				}
				return tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: roleID, GatewayRef: gatewayID, AppID: appID, IdentityGeneration: 1}, 0)
			})
			a.teamRuntime.ingress, err = workingress.New(store, workingress.Config{Directory: teamDirectory{app: a, store: store}})
			if err != nil {
				t.Fatal(err)
			}
			a.teamRuntime.active.Store(true)
			var frozenCard string
			var mu sync.Mutex
			observations := 0
			connected := make(chan *websocket.Conn, 1)
			const chat, original, cardID = "oc_sdk_journey", "om_original", "om_choose_leader"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case larkws.GenEndpointUri:
					_ = json.NewEncoder(w).Encode(larkws.EndpointResp{Code: 0, Data: &larkws.Endpoint{Url: "ws://" + r.Host + "/ws?device_id=journey&service_id=1"}})
				case "/ws":
					upgrader := websocket.Upgrader{}
					ws, e := upgrader.Upgrade(w, r, nil)
					if e != nil {
						t.Error(e)
						return
					}
					connected <- ws
				case larkcore.TenantAccessTokenInternalUrlPath:
					_, _ = fmt.Fprintf(w, `{"code":0,"tenant_access_token":%q,"expire":7200}`, "token-"+appID)
				case "/open-apis/im/v1/messages/" + original + "/reply":
					var body map[string]any
					if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
						t.Error(e)
					}
					mu.Lock()
					frozenCard, _ = body["content"].(string)
					mu.Unlock()
					if scenario.loseReply {
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = fmt.Fprint(w, `{"code":1,"msg":"reply unavailable"}`)
						return
					}
					_, _ = fmt.Fprintf(w, `{"code":0,"data":{"message_id":%q,"chat_id":%q}}`, cardID, chat)
				case "/open-apis/im/v1/messages/" + cardID:
					mu.Lock()
					defer mu.Unlock()
					observations++
					observed := frozenCard
					if r.URL.Query().Get("card_msg_content_type") != "user_card_content" {
						t.Error("message read must request the documented original card JSON")
						observed = `{"title":"选择项目负责人","elements":[]}`
					}
					if scenario.differentCard {
						observed = strings.Replace(frozenCard, "选择项目负责人", "另一张卡", 1)
					}
					_, _ = fmt.Fprintf(w, `{"code":0,"data":{"items":[{"message_id":%q,"chat_id":%q,"parent_id":%q,"msg_type":"interactive","sender":{"id":%q,"id_type":"app_id","sender_type":"app"},"body":{"content":%q}}]}}`, cardID, chat, original, appID, observed)
				default:
					t.Errorf("unexpected gateway HTTP request %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			controller := feishu.NewMultiGatewayController()
			a.gateway = controller
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			if err = controller.UpsertApp(ctx, feishu.GatewayAppConfig{GatewayID: gatewayID, AppID: appID, AppSecret: "isolated",
				Domain: server.URL, Enabled: true, BotOpenID: "ou_bot", RawIngressHook: a.teamIngressHook(),
				PreviewCacheDir: t.TempDir(), PreviewStatePath: filepath.Join(t.TempDir(), "preview.json")}); err != nil {
				cancel()
				t.Fatal(err)
			}
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
				t.Fatal("no controlled Feishu WebSocket connection")
			}
			header := func(id, event string) map[string]string {
				return map[string]string{"event_id": id, "event_type": event, "app_id": appID, "tenant_key": "tenant"}
			}
			bootstrapSDKEvent(t, ws, "incoming", header("incoming", "im.message.receive_v1"), map[string]any{
				"sender":  map[string]any{"sender_id": map[string]string{"open_id": "ou_human"}, "sender_type": "user", "tenant_key": "tenant"},
				"message": map[string]string{"chat_id": chat, "chat_type": "group", "message_id": original, "message_type": "text", "content": `{"text":"建立客户门户，先做订单查询。"}`},
			})
			result, err := a.teamRuntime.ingress.ProcessPending(t.Context(), 16)
			if err != nil || len(result.Selections) != 1 {
				t.Fatalf("group establishment selection %+v: %v", result, err)
			}
			selection := result.Selections[0]
			if err = a.presentTeamSelection(t.Context(), selection); (err != nil) != scenario.loseReply {
				t.Fatalf("expected unknown=%t: %v", scenario.loseReply, err)
			}
			mu.Lock()
			card := frozenCard
			mu.Unlock()
			var parsed struct {
				Body struct {
					Elements []struct {
						Behaviors []struct {
							Value map[string]any `json:"value"`
						} `json:"behaviors"`
					} `json:"elements"`
				} `json:"body"`
			}
			if err = json.Unmarshal([]byte(card), &parsed); err != nil {
				t.Fatal(err)
			}
			callback := parsed.Body.Elements[1].Behaviors[0].Value
			ack := bootstrapSDKEvent(t, ws, "click", header("click", "card.action.trigger"), map[string]any{
				"operator": map[string]string{"open_id": "ou_human", "tenant_key": "tenant"},
				"context":  map[string]string{"open_chat_id": chat, "open_message_id": cardID},
				"action":   map[string]any{"value": callback},
			})
			if !strings.Contains(string(ack.Data), "已收到") {
				t.Fatalf("missing visible SDK callback acknowledgement: %+v", ack)
			}
			var clicks []workstore.HumanInteraction
			pickerTx(t, a, func(tx *workstore.Tx) error {
				var e error
				clicks, e = tx.ListPendingHumanInteractions(8)
				return e
			})
			if len(clicks) != 1 {
				t.Fatalf("expected one persisted card click, got %d", len(clicks))
			}
			if err = a.processTeamInteraction(t.Context(), clicks[0]); (err != nil) != scenario.differentCard {
				t.Fatal(err)
			}
			if scenario.differentCard {
				pickerTx(t, a, func(tx *workstore.Tx) error {
					notice, err := tx.GetSelectionNotice(selection.IngressID)
					if err != nil {
						return err
					}
					if notice.State != work.OperationUnknown || notice.ExternalMessageID != "" {
						return fmt.Errorf("different card forged a delivery receipt: %+v", notice)
					}
					return nil
				})
				return
			}
			result, err = a.teamRuntime.ingress.ProcessPending(t.Context(), 16)
			if err != nil || result.Processed != 1 {
				t.Fatalf("original SDK input did not resume: %+v %v", result, err)
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				notice, err := tx.GetSelectionNotice(selection.IngressID)
				if err != nil {
					return err
				}
				if notice.ExternalMessageID != cardID || notice.State != work.OperationConfirmed || (len(notice.Observation) > 0) != scenario.loseReply {
					return fmt.Errorf("wrong selection receipt: %+v", notice)
				}
				return nil
			})
			mu.Lock()
			defer mu.Unlock()
			if (observations == 1) != scenario.loseReply {
				t.Fatalf("unexpected exact message reads: %d", observations)
			}
		})
	}
}

func bootstrapSDKEvent(t *testing.T, conn *websocket.Conn, id string, header map[string]string, event any) *larkws.Response {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"schema": "2.0", "header": header, "event": event})
	if err != nil {
		t.Fatal(err)
	}
	frame := larkws.Frame{Method: int32(larkws.FrameTypeData), Payload: payload, Headers: []larkws.Header{
		{Key: larkws.HeaderType, Value: string(larkws.MessageTypeEvent)}, {Key: larkws.HeaderMessageID, Value: id},
	}}
	wire, err := frame.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err = conn.WriteMessage(websocket.BinaryMessage, wire); err != nil {
		t.Fatal(err)
	}
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if err = frame.Unmarshal(raw); err != nil {
			t.Fatal(err)
		}
		if larkws.Headers(frame.Headers).GetString(larkws.HeaderMessageID) != id {
			continue
		}
		var reply larkws.Response
		if err = json.Unmarshal(frame.Payload, &reply); err != nil || reply.StatusCode != http.StatusOK {
			t.Fatalf("SDK ack %s: %v", frame.Payload, err)
		}
		return &reply
	}
}
