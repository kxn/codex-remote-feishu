package codexprofile

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
)

func TestRunNativeConfigProbeSessionReadsConnectionEvidenceWithoutThread(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	serverDone := make(chan error, 1)
	go func() {
		defer server.Close()
		scanner := bufio.NewScanner(server)
		for scanner.Scan() {
			var frame map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
				serverDone <- err
				return
			}
			method, _ := frame["method"].(string)
			if strings.HasPrefix(method, "thread/") || strings.HasPrefix(method, "turn/") {
				serverDone <- &testCapabilityError{message: "native config probe created runtime state"}
				return
			}
			id, hasID := frame["id"].(string)
			if !hasID {
				continue
			}
			result := map[string]any{}
			switch method {
			case "initialize":
				result["userAgent"] = "codex-cli/1.2.3"
			case "config/read":
				result["config"] = map[string]any{
					"model_provider":   "team-native",
					"chatgpt_base_url": OfficialChatGPTBaseURL,
					"model_providers": map[string]any{
						"team-native": map[string]any{"base_url": "https://native.example/v1", "env_key": "CUSTOM_API_KEY"},
						"other":       map[string]any{"base_url": "https://other.example/v1", "env_key": "OTHER_API_KEY"},
					},
				}
			default:
				serverDone <- &testCapabilityError{message: "unexpected method: " + method}
				return
			}
			if err := writeProbeTestFrame(server, map[string]any{"id": id, "result": result}); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- scanner.Err()
	}()

	observation, err := RunNativeConfigProbeSession(context.Background(), client, client, "test-version", "/tmp/native-probe")
	if err != nil {
		t.Fatalf("RunNativeConfigProbeSession: %v", err)
	}
	if observation.ModelProviderID != "team-native" || observation.ModelEndpoint != "https://native.example/v1" || observation.ChatGPTEndpoint != OfficialChatGPTBaseURL {
		t.Fatalf("unexpected native observation: %#v", observation)
	}
	if strings.Join(observation.ProviderIDs, ",") != "other,team-native" {
		t.Fatalf("provider IDs = %#v", observation.ProviderIDs)
	}
	if strings.Join(observation.ProviderEnvKeys, ",") != "CUSTOM_API_KEY,OTHER_API_KEY" {
		t.Fatalf("provider env keys = %#v", observation.ProviderEnvKeys)
	}

	client.Close()
	if err := <-serverDone; err != nil && err != io.ErrClosedPipe {
		t.Fatalf("native probe server: %v", err)
	}
}

func TestProjectNativeConnectionEvidenceRedactsUnsafeEndpoints(t *testing.T) {
	observation := NativeConfigObservation{
		ModelProviderID: "team-native",
		ModelEndpoint:   "https://user:secret@native.example/v1?token=secret",
		ChatGPTEndpoint: "https://chatgpt.example/backend-api/#secret",
	}
	evidence := ProjectNativeConnectionEvidence(observation, 42)
	if evidence.ConnectionGeneration != 42 || evidence.Revision != 1 {
		t.Fatalf("unexpected generation evidence: %#v", evidence)
	}
	if !strings.Contains(evidence.ModelEndpointID, "opaque") || !strings.Contains(evidence.ChatGPTEndpointID, "opaque") {
		t.Fatalf("unsafe endpoints were not made opaque: %#v", evidence)
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	for _, secret := range []string{"user", "secret", "token", "chatgpt.example"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("public evidence leaked %q: %s", secret, raw)
		}
	}
	if _, err := json.Marshal(observation); err == nil {
		t.Fatal("raw native config observation must reject JSON serialization")
	}
}
