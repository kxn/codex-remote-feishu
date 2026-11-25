package daemon

import (
	"context"
	"errors"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// structuredUATStager runs the actual SDK planner with deterministic local
// download/quote fixtures. The rich-post parser callback is controlled here;
// production LiveGateway's post decoder has separate adapter tests.
type structuredUATStager struct {
	dir              string
	started, release chan struct{}
	startOnce        sync.Once
}

func newStructuredUATStager(t *testing.T) *structuredUATStager {
	t.Helper()
	d := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "one.png", "two.png", "post.png"} {
		if err := os.WriteFile(filepath.Join(d, name), []byte("fixture-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return &structuredUATStager{dir: d, started: make(chan struct{}), release: make(chan struct{})}
}
func (s *structuredUATStager) StagePrivateMessage(ctx context.Context, raw platformingress.RawIngressEnvelope, _ string) (control.Action, bool, error) {
	event := privateJourneySDKText(raw.MessageID, "")
	event.Event.Message.ChatId = privateJourneyString(raw.ChatID)
	event.Event.Message.MessageType = privateJourneyString(raw.MessageType)
	event.Event.Message.Content = privateJourneyString(raw.RawContent)
	event.Event.Message.ParentId = privateJourneyString(raw.ParentID)
	event.Event.Sender.SenderId.OpenId = privateJourneyString(raw.SenderIDs.OpenID)
	env := gatewaypkg.InboundEnv{GatewayID: raw.GatewayRef, ParseTextActionWithoutCatalog: control.ParseFeishuTextActionWithoutCatalog, RecordSurfaceMessage: func(string, string) {}, DownloadImage: func(ctx context.Context, _, key string) (string, string, error) {
		if key == "bad" {
			s.startOnce.Do(func() { close(s.started) })
			select {
			case <-s.release:
			case <-ctx.Done():
				return "", "", ctx.Err()
			}
			return "", "", errors.New("controlled image download failure")
		}
		n := map[string]string{"one": "one.png", "two": "two.png", "post-image": "post.png"}[key]
		if n == "" {
			return "", "", errors.New("unknown image")
		}
		return filepath.Join(s.dir, n), "image/png", nil
	}, DownloadFile: func(_ context.Context, _, key, name string) (string, error) {
		if key != "file-a" && key != "file-b" {
			return "", errors.New("unknown file")
		}
		return filepath.Join(s.dir, name), nil
	}, QuotedMessageInputs: func(_ context.Context, m *larkim.EventMessage) gatewaypkg.QuotedMessageInputs {
		if m.ParentId != nil && *m.ParentId == "quoted-original" {
			return gatewaypkg.QuotedMessageInputs{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "quoted original"}}}
		}
		return gatewaypkg.QuotedMessageInputs{}
	}, ParsePostInputs: func(_ context.Context, _, content string) ([]agentproto.Input, string, error) {
		if !strings.Contains(content, "rich text") {
			return nil, "", errors.New("unexpected post")
		}
		return []agentproto.Input{{Type: agentproto.InputText, Text: "title"}, {Type: agentproto.InputText, Text: "rich text"}, {Type: agentproto.InputLocalImage, Path: filepath.Join(s.dir, "post.png"), MIMEType: "image/png"}}, "title rich text", nil
	}}
	return gatewaypkg.ParsePlannedInboundAction(ctx, env, event)
}
func prepareStructuredUATInputs(t *testing.T, ctx context.Context, r *applicationRuntime, raw platformingress.RawIngressEnvelope) {
	t.Helper()
	surface := gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")
	owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: surface, ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	inputs := []struct{ id, kind, body string }{{"file-a", "file", `{"file_key":"file-a","file_name":"a.txt"}`}, {"file-b", "file", `{"file_key":"file-b","file_name":"b.txt"}`}, {"image-one", "image", `{"image_key":"one"}`}, {"image-two", "image", `{"image_key":"two"}`}}
	for _, v := range inputs {
		m := raw
		m.EventID = v.id
		m.MessageID = v.id
		m.MessageType = v.kind
		m.RawContent = v.body
		m.ReceivedAt = time.Now()
		if err := r.hook.Receive(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	for {
		cp, e := r.journal.LoadCheckpoint(ctx, owner, 1)
		if e == nil && len(cp.Core.StagedFiles) == 2 && len(cp.Core.StagedImages) == 2 {
			ready := true
			for _, v := range cp.Core.StagedFiles {
				if v.StagePending {
					ready = false
				}
			}
			if ready {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("media inputs not staged")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
