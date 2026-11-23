package feishu

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestPrivateMediaUsesLegacySDKParserOnlyInStaging(t *testing.T) {
	hook := startupTestHook(t)
	g := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app", RawIngressHook: &hook})
	g.mu.Lock()
	g.ingressStarted = true
	g.mu.Unlock()
	calls := 0
	g.downloadImageFn = func(_ context.Context, message, key string) (string, string, error) {
		calls++
		if message != "message" || key != "image-key" {
			t.Fatal("lost SDK identity")
		}
		return "/tmp/verified-image.png", "image/png", nil
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", SenderTenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "message", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "image", RawContent: `{"image_key":"image-key"}`, ReceivedAt: time.Now()}
	if _, _, err := DecodePrivateMessage(t.Context(), raw); !errors.Is(err, gatewaypkg.ErrInboundStaging) {
		t.Fatalf("media did not defer: %v", err)
	}
	if calls != 0 {
		t.Fatal("decoder performed I/O")
	}
	action, ok, err := g.StagePrivateMessage(t.Context(), raw, "router")
	if err != nil || !ok || action.Kind != control.ActionImageMessage || action.LocalPath != "/tmp/verified-image.png" || calls != 1 {
		t.Fatalf("staging: %+v %v", action, err)
	}
}

func TestPrivateProjectSDKCommandsAndCardConfirmation(t *testing.T) {
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", SenderTenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "message", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/project"}`, ReceivedAt: time.Now()}
	action, ok, err := DecodePrivateMessage(t.Context(), raw)
	if err != nil || !ok || action.Kind != control.ActionPrivateProject || action.Text != "/project" {
		t.Fatalf("project message: %#v %v %v", action, ok, err)
	}
	card := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "card", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "chat", MessageID: "card-message", Kind: "card", Values: frontstagecontract.ActionPayloadPageLocalAction(string(control.ActionPrivateProject), "confirm draft digest")}
	action, ok = DecodePrivateCard(card, "surface")
	if !ok || action.Kind != control.ActionPrivateProject || action.Text != "/project confirm draft digest" || action.Inbound == nil || !action.Inbound.CardCallback {
		t.Fatalf("project card: %#v %v", action, ok)
	}
}

func TestPrivateMediaStagingUsesEventManifest(t *testing.T) {
	hook := startupTestHook(t)
	g := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app", RawIngressHook: &hook, TempDir: t.TempDir()})
	g.mu.Lock()
	g.ingressStarted = true
	g.mu.Unlock()
	g.downloadImageFn = func(ctx context.Context, _, _ string) (string, string, error) {
		store := privatestaging.FromContext(ctx)
		if store == nil {
			t.Fatal("missing private staging context")
		}
		path, err := store.Create(strings.NewReader("image contents"), ".png")
		return path, "image/png", err
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "message", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "image", RawContent: `{"image_key":"image-key"}`, ReceivedAt: time.Now()}
	action, ok, err := g.StagePrivateMessage(t.Context(), raw, "router")
	if err != nil || !ok {
		t.Fatalf("stage: %v", err)
	}
	body, err := os.ReadFile(action.LocalPath)
	if err != nil || string(body) != "image contents" {
		t.Fatalf("staged input: %q %v", body, err)
	}
	owner := privatestaging.Owner{Tenant: "tenant", AppID: "app", Gateway: "gateway", EventID: "event", MessageID: "message"}
	if _, err = privatestaging.Open(g.config.TempDir, owner); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateRecoveryCommandsReachAuthenticatedTextLane(t *testing.T) {
	for _, text := range []string{"/recover", "/recover 1", "/recover confirm 1"} {
		raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", SenderTenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "message", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"` + text + `"}`, ReceivedAt: time.Now()}
		action, ok, err := DecodePrivateMessage(t.Context(), raw)
		if err != nil || !ok || action.Kind != control.ActionTextMessage || action.Text != text || action.ActorUserID != "user" {
			t.Fatalf("recovery SDK decode: %#v %v %v", action, ok, err)
		}
	}
}
