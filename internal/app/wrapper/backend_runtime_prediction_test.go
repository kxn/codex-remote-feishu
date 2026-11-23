package wrapper

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestWrapperPredictionCapabilityIsExplicitCodexOnly(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude, agentproto.BackendOpenCode} {
		runtime := newBackendRuntime(Config{Backend: backend, InstanceID: "test-instance"})
		if got := runtime.Capabilities().ThreadPrediction; got != (backend == agentproto.BackendCodex) {
			t.Fatalf("%s wrapper prediction capability = %v", backend, got)
		}
		if agentproto.DefaultCapabilitiesForBackend(backend).ThreadPrediction {
			t.Fatalf("legacy %s must not gain prediction support", backend)
		}
	}
}
