package feishu

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func startupTestHook(t *testing.T) RawIngressHook {
	t.Helper()
	h, err := PrepareStartupIngressHook("router", RawIngressHook{Authorize: func(context.Context, platformingress.RawIngressEnvelope) (bool, error) { return true, nil }, Receive: func(context.Context, platformingress.RawIngressEnvelope) error { return nil }, ReceiveInteractionResponse: func(context.Context, platformingress.HumanInteractionEnvelope) (InteractionResponse, error) {
		return InteractionResponse{}, nil
	}, ReceiveMenu: func(context.Context, MenuIngressEvent) error { return nil }, ReceivePlatformEvent: func(context.Context, PlatformIngressEvent) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func TestStartupIngressProofRequiresInitialHookAndRejectsReplacement(t *testing.T) {
	hook := startupTestHook(t)
	g := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app", RawIngressHook: &hook})
	if _, err := g.StartupIngressProof(); err == nil {
		t.Fatal("unstarted gateway proved active")
	}
	g.mu.Lock()
	g.ingressStarted = true
	g.mu.Unlock()
	proof, err := g.StartupIngressProof()
	if err != nil {
		t.Fatal(err)
	}
	if err = g.VerifyStartupIngressProof(proof); err != nil {
		t.Fatal(err)
	}
	bad := proof
	bad.Nonce = "other"
	if g.VerifyStartupIngressProof(bad) == nil {
		t.Fatal("forged startup nonce accepted")
	}
	if err = g.SetRawIngressHook(startupTestHook(t)); err == nil {
		t.Fatal("running hook replaced")
	}
	g.mu.Lock()
	g.ingressStarted = false
	g.mu.Unlock()
	if g.VerifyStartupIngressProof(proof) == nil {
		t.Fatal("stopped gateway retained proof")
	}
	legacy := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app"})
	if err = legacy.SetRawIngressHook(hook); err == nil {
		t.Fatal("late startup hook accepted")
	}
}
