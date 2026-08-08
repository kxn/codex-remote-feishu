package feishu

import (
	"os"
	"strings"
	"testing"
)

func TestGatewayRuntimeRegistersBotAddedToGroupEvent(t *testing.T) {
	data, err := os.ReadFile("gateway_runtime.go")
	if err != nil {
		t.Fatalf("read gateway_runtime.go: %v", err)
	}
	source := string(data)
	for _, needle := range []string{
		"dispatch.OnP2ChatMemberBotAddedV1",
		"gatewaypkg.ParseBotAddedToGroupEvent",
		"handleGatewayEventAction(ctx, action, handler)",
	} {
		if !strings.Contains(source, needle) {
			t.Fatalf("gateway runtime missing %q", needle)
		}
	}
}
