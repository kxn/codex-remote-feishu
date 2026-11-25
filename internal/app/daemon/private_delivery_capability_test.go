package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestPrivateDeliveryCapabilityIsScopedToAuthenticatedConnection(t *testing.T) {
	h := &privateNativeHost{deliveryCapabilities: map[string]privateDeliveryCapability{"instance": {connection: 2, version: agentproto.PrivateDeliveryVersion}}}
	for _, kind := range []agentproto.CommandKind{agentproto.CommandRequestRespond, agentproto.CommandReviewStart, agentproto.CommandThreadCompactStart, agentproto.CommandThreadRead} {
		if !h.privateDeliverySupported("instance", 2, kind) {
			t.Fatalf("new connection rejected %s", kind)
		}
		if h.privateDeliverySupported("instance", 1, kind) || h.privateDeliverySupported("other", 2, kind) {
			t.Fatalf("old or foreign connection accepted %s", kind)
		}
	}
	h.deliveryCapabilities["instance"] = privateDeliveryCapability{connection: 3, version: 0}
	if h.privateDeliverySupported("instance", 3, agentproto.CommandRequestRespond) {
		t.Fatal("legacy connection allowed capability-dependent command")
	}
	for _, kind := range []agentproto.CommandKind{agentproto.CommandPromptSend, agentproto.CommandTurnSteer, agentproto.CommandTurnInterrupt, agentproto.CommandThreadShellCommand} {
		if !h.privateDeliverySupported("instance", 3, kind) {
			t.Fatalf("legacy operation blocked: %s", kind)
		}
	}
	h.deliveryCapabilities["instance"] = privateDeliveryCapability{connection: 4, version: agentproto.PrivateDeliveryVersionControlReceipts}
	if h.privateDeliverySupported("instance", 4, agentproto.CommandReviewStart) {
		t.Fatal("v1 wrapper allowed isolated review")
	}
	for _, kind := range []agentproto.CommandKind{agentproto.CommandPromptSend, agentproto.CommandRequestRespond, agentproto.CommandThreadCompactStart, agentproto.CommandThreadRead} {
		if !h.privateDeliverySupported("instance", 4, kind) {
			t.Fatalf("v1 wrapper unexpectedly blocked %s", kind)
		}
	}
}
