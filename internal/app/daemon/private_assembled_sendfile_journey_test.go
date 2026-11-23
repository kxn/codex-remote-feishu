package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

type assembledFileGateway struct {
	*messageIDAssigningGateway
	sender feishu.IMFileSender
}

func (g assembledFileGateway) SendIMFile(ctx context.Context, request feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
	return g.sender.SendIMFile(ctx, request)
}

func setupAssembledSDKFileGateway(t *testing.T, app *App, cards *messageIDAssigningGateway) <-chan string {
	t.Helper()
	sent := make(chan string, 4)
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/callback/ws/endpoint":
			http.NotFound(w, r)
		case "/open-apis/bot/v3/info":
			fmt.Fprint(w, `{"code":0,"bot":{"open_id":"ou_bot"}}`)
		case "/open-apis/auth/v3/tenant_access_token/internal":
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"fixture-token","expire":7200}`)
		case "/open-apis/im/v1/files":
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(w, "file", 400)
				return
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil || string(data) != "selected bytes" || r.FormValue("file_name") != "report.txt" {
				t.Errorf("file body %q filename %q err %v", data, r.FormValue("file_name"), err)
			}
			fmt.Fprint(w, `{"code":0,"data":{"file_key":"fixture-file"}}`)
		case "/open-apis/im/v1/messages":
			var body struct {
				ReceiveID string `json:"receive_id"`
				Kind      string `json:"msg_type"`
				Content   string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.ReceiveID != "private-chat" || body.Kind != "file" || body.Content != `{"file_key":"fixture-file"}` {
				t.Errorf("wrong file message: %+v", body)
			}
			sent <- body.ReceiveID
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"file-receipt","chat_id":"private-chat"}}`)
		default:
			t.Errorf("unexpected SDK route: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(platform.Close)
	controller := feishu.NewMultiGatewayController()
	if err := controller.UpsertApp(t.Context(), feishu.GatewayAppConfig{GatewayID: "gateway", AppID: "app", AppSecret: "fixture", Domain: platform.URL, Enabled: true, ImageTempDir: t.TempDir(), PreviewCacheDir: t.TempDir(), PreviewStatePath: filepath.Join(t.TempDir(), "preview.json")}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); _ = controller.Start(ctx, nil) }()
	t.Cleanup(func() { cancel(); <-done })
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := controller.ProjectGateway("gateway", "app"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SDK file gateway unavailable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.gateway = assembledFileGateway{messageIDAssigningGateway: cards, sender: controller}
	return sent
}

func findPrivateCallback(v any, kind string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == kind && x["private_view"] != nil {
			return x
		}
		for _, child := range x {
			if hit := findPrivateCallback(child, kind); hit != nil {
				return hit
			}
		}
	case []any:
		for _, child := range x {
			if hit := findPrivateCallback(child, kind); hit != nil {
				return hit
			}
		}
	}
	return nil
}
func exerciseAssembledSDKSendFile(t *testing.T, ctx context.Context, app *App, gateway *messageIDAssigningGateway, raw platformingress.RawIngressEnvelope, cwd string, sent <-chan string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cwd, "report.txt"), []byte("selected bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	before := len(gateway.snapshotOperations())
	raw.EventID = "sendfile"
	raw.MessageID = "sendfile-source"
	raw.RawContent = `{"text":"/sendfile"}`
	if err := app.applications.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	var card feishu.Operation
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		ops := gateway.snapshotOperations()
		for _, op := range ops[before:] {
			if op.Kind == feishu.OperationSendCard && strings.Contains(op.CardTitle, "选择要发送的文件") {
				card = op
				break
			}
		}
		if card.MessageID != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if card.MessageID == "" {
		t.Logf("operations: %+v", gateway.snapshotOperations()[before:])
		t.Fatal("SDK sendfile picker not published")
	}
	for {
		if _, err := app.applications.router.PublishedMessage(ctx, "tenant", "app", "private-chat", card.MessageID); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("file picker receipt not registered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	frozen, err := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err = json.Unmarshal(frozen, &doc); err != nil {
		t.Fatal(err)
	}
	selectValue := findPrivateCallback(doc, "path_picker_select")
	if selectValue == nil {
		t.Fatalf("file selector missing: %s", frozen)
	}
	selectValue["entry_name"] = "report.txt"
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "file-select", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: selectValue, ReceivedAt: time.Now()}
	response, err := app.applications.hook.ReceiveInteractionResponse(ctx, click)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(response.RawBody, &doc); err != nil {
		t.Fatalf("select response: %s %v", response.RawBody, err)
	}
	confirm := findPrivateCallback(doc, "path_picker_confirm")
	if confirm == nil {
		t.Fatalf("confirm missing: %s", response.RawBody)
	}
	click.EventID = "file-confirm"
	click.Values = confirm
	response, err = app.applications.hook.ReceiveInteractionResponse(ctx, click)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.applications.hook.ReceiveInteractionResponse(ctx, click); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
	case <-ctx.Done():
		t.Fatal("SDK file message not sent")
	}
	select {
	case <-sent:
		t.Fatal("replayed callback resent file")
	case <-time.After(100 * time.Millisecond):
	}
}
