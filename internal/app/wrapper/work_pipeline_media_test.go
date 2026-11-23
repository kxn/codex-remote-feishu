package wrapper

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func TestSDKImageQueueEngineWrapperReceivesActualNativeImages(t *testing.T) {
	binary := buildPipelineTeamCLI(t)
	t.Setenv("FSC_NATIVE_PIPELINE_IMAGES", "true")
	for _, backend := range []work.Backend{work.BackendCodex, work.BackendClaude} {
		for _, mode := range []string{"separate-images", "captioned-post", "image-only-post"} {
			t.Run(string(backend)+"/"+mode, func(t *testing.T) {
				t.Setenv("FSC_NATIVE_PIPELINE_MEDIA_POST", mode)
				runSDKNativePipeline(t, binary, backend)
			})
		}
	}
}

func pipelineMediaBytes(index int) []byte {
	pic := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pic.SetNRGBA(0, 0, color.NRGBA{R: uint8(index * 80), G: 40, A: 255})
	var b bytes.Buffer
	_ = png.Encode(&b, pic)
	return b.Bytes()
}

type pipelineMediaExpander struct{ gateway *feishu.LiveGateway }

func (e pipelineMediaExpander) Expand(ctx context.Context, raw work.RawIngressEnvelope) (workingress.ExpandedMessage, error) {
	expanded, err := e.gateway.ExpandTeamMessage(ctx, raw)
	result := workingress.ExpandedMessage{Text: expanded.Text, Complete: expanded.Complete, Issues: expanded.Issues}
	for _, asset := range expanded.Attachments {
		result.Attachments = append(result.Attachments, workingress.ExpandedAttachment{Kind: asset.Kind, MediaType: asset.MediaType, SourceMessageID: asset.SourceMessageID, FileName: asset.FileName, Data: asset.Data})
	}
	return result, err
}

func pipelineMediaSDK(t *testing.T, ctx context.Context, receive func(context.Context, work.RawIngressEnvelope) error) (workingress.Expander, func(work.RawIngressEnvelope)) {
	t.Helper()
	connected := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == larkws.GenEndpointUri:
			_ = json.NewEncoder(w).Encode(larkws.EndpointResp{Code: 0, Data: &larkws.Endpoint{Url: "ws://" + r.Host + "/ws?device_id=media&service_id=1"}})
		case r.URL.Path == "/ws":
			ws, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
			if e != nil {
				t.Error(e)
				return
			}
			connected <- ws
		case r.URL.Path == "/open-apis/bot/v3/info":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"code":0,"bot":{"open_id":"pipeline-bot","app_name":"fixture"}}`)
		case r.URL.Path == larkcore.TenantAccessTokenInternalUrlPath:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"pipeline-media-token","expire":7200}`)
		case strings.Contains(r.URL.Path, "/resources/"):
			index := 1
			if strings.Contains(r.URL.Path, "image-two") {
				index = 2
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pipelineMediaBytes(index))
		default:
			t.Errorf("unexpected media SDK route %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	gateway := feishu.NewLiveGateway(feishu.LiveGatewayConfig{GatewayID: "pipeline-gateway", AppID: "pipeline-app", AppSecret: "fixture", BotOpenID: "pipeline-bot", Domain: server.URL, TempDir: t.TempDir(), RawIngressHook: &feishu.RawIngressHook{Authorize: func(context.Context, work.RawIngressEnvelope) (bool, error) { return true, nil }, Receive: receive}})
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- gateway.Start(runCtx, nil) }()
	t.Cleanup(func() {
		cancel()
		if e := <-done; e != nil {
			t.Error(e)
		}
	})
	var ws *websocket.Conn
	select {
	case ws = <-connected:
	case <-time.After(3 * time.Second):
		t.Fatal("actual Feishu SDK did not connect")
	}
	t.Cleanup(func() { _ = ws.Close() })
	send := func(raw work.RawIngressEnvelope) {
		t.Helper()
		payload, e := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]string{"event_id": raw.EventID, "event_type": "im.message.receive_v1", "app_id": raw.AppID, "tenant_key": raw.Tenant}, "event": map[string]any{"sender": map[string]any{"sender_id": map[string]string{"open_id": raw.SenderIDs.OpenID}, "sender_type": "user", "tenant_key": raw.Tenant}, "message": map[string]any{"message_id": raw.MessageID, "chat_id": raw.ChatID, "chat_type": "p2p", "message_type": raw.MessageType, "content": raw.RawContent}}})
		if e != nil {
			t.Fatal(e)
		}
		frame := larkws.Frame{Method: int32(larkws.FrameTypeData), Payload: payload, Headers: []larkws.Header{{Key: larkws.HeaderType, Value: string(larkws.MessageTypeEvent)}, {Key: larkws.HeaderMessageID, Value: raw.EventID}}}
		wire, e := frame.Marshal()
		if e != nil {
			t.Fatal(e)
		}
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		if e = ws.WriteMessage(websocket.BinaryMessage, wire); e != nil {
			t.Fatal(e)
		}
		for {
			_, reply, e := ws.ReadMessage()
			if e != nil {
				t.Fatal(e)
			}
			if e = frame.Unmarshal(reply); e != nil {
				t.Fatal(e)
			}
			if larkws.Headers(frame.Headers).GetString(larkws.HeaderMessageID) != raw.EventID {
				continue
			}
			var ack larkws.Response
			if e = json.Unmarshal(frame.Payload, &ack); e != nil || ack.StatusCode != 200 {
				t.Fatal("SDK did not acknowledge durable media input")
			}
			break
		}
	}
	return pipelineMediaExpander{gateway}, send
}

// Runs inside the real controlled Codex/Claude child, on the actual native frame.
func pipelineVerifyMediaFrame(frame map[string]any) error {
	var actual [][]byte
	var visit func(any) error
	visit = func(value any) error {
		switch v := value.(type) {
		case map[string]any:
			if v["type"] == "localImage" {
				path, _ := v["path"].(string)
				data, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				actual = append(actual, data)
				return nil
			}
			if v["type"] == "image" {
				source, _ := v["source"].(map[string]any)
				encoded, _ := source["data"].(string)
				if source["media_type"] != "image/png" {
					return fmt.Errorf("native image MIME type was lost")
				}
				data, e := base64.StdEncoding.DecodeString(encoded)
				if e != nil {
					return e
				}
				actual = append(actual, data)
				return nil
			}
			for _, child := range v {
				if e := visit(child); e != nil {
					return e
				}
			}
		case []any:
			for _, child := range v {
				if e := visit(child); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e := visit(frame); e != nil {
		return e
	}
	if len(actual) != 2 {
		return fmt.Errorf("native child received %d actual images, need 2", len(actual))
	}
	for i, data := range actual {
		if !bytes.Equal(data, pipelineMediaBytes(i+1)) {
			return fmt.Errorf("native image bytes or order changed")
		}
	}
	envelope, ok := pipelineFindEnvelope(frame)
	want := 3
	if os.Getenv("FSC_NATIVE_PIPELINE_MEDIA_POST") == "captioned-post" {
		want = 1
	}
	if os.Getenv("FSC_NATIVE_PIPELINE_MEDIA_POST") == "image-only-post" {
		want = 2
	}
	if !ok || len(envelope.Inputs) != want {
		return fmt.Errorf("native image prompt lost queued image/text provenance")
	}
	return nil
}
