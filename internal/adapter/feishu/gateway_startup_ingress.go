package feishu

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// StartupIngressProof describes a running gateway whose neutral hook was
// installed before Start. Its nonce is checked against the actual live object.
type StartupIngressProof struct {
	GatewayID string `json:"gateway_id"`
	AppID     string `json:"app_id"`
	RouterID  string `json:"router_id"`
	Nonce     string `json:"nonce"`
}

func PrepareStartupIngressHook(routerID string, hook RawIngressHook) (RawIngressHook, error) {
	if routerID == "" || hook.Authorize == nil || hook.Receive == nil || hook.ReceiveInteractionResponse == nil || hook.ReceiveMenu == nil || hook.ReceivePlatformEvent == nil {
		return RawIngressHook{}, fmt.Errorf("complete neutral startup hook required")
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return RawIngressHook{}, err
	}
	hook.startupRouterID = routerID
	hook.startupNonce = hex.EncodeToString(nonce)
	return hook, nil
}
func (g *LiveGateway) StartupIngressProof() (StartupIngressProof, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.ingressStarted || !g.startupHookAtConstruction || g.rawIngressHook == nil || g.rawIngressHook.startupRouterID == "" || g.rawIngressHook.startupNonce == "" {
		return StartupIngressProof{}, fmt.Errorf("neutral hook did not start with gateway")
	}
	return StartupIngressProof{GatewayID: g.config.GatewayID, AppID: g.config.AppID, RouterID: g.rawIngressHook.startupRouterID, Nonce: g.rawIngressHook.startupNonce}, nil
}
func (g *LiveGateway) VerifyStartupIngressProof(proof StartupIngressProof) error {
	actual, err := g.StartupIngressProof()
	if err != nil {
		return err
	}
	if actual != proof {
		return fmt.Errorf("gateway ingress proof does not match live startup")
	}
	return nil
}

// VerifyStartupIngressProof checks the current controller worker, not a stale
// gateway object retained after a restart/reconfiguration.
func (c *MultiGatewayController) VerifyStartupIngressProof(proof StartupIngressProof) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.workers[proof.GatewayID]
	if w == nil || w.runtime == nil || w.config.AppID != proof.AppID {
		return fmt.Errorf("gateway worker changed")
	}
	g, ok := w.runtime.(*LiveGateway)
	if !ok {
		return fmt.Errorf("gateway runtime cannot prove startup ingress")
	}
	return g.VerifyStartupIngressProof(proof)
}
func (c *MultiGatewayController) StartupIngressProof(gateway string) (StartupIngressProof, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.workers[gateway]
	if w == nil || w.runtime == nil {
		return StartupIngressProof{}, fmt.Errorf("gateway worker unavailable")
	}
	g, ok := w.runtime.(*LiveGateway)
	if !ok {
		return StartupIngressProof{}, fmt.Errorf("gateway runtime cannot prove startup ingress")
	}
	return g.StartupIngressProof()
}
