package codexprofile

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

func TestRunCapabilityPreflightSessionVerifiesIsolatedRuntimeContract(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	var (
		mu      sync.Mutex
		methods []string
	)
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
			mu.Lock()
			methods = append(methods, method)
			mu.Unlock()
			id, hasID := frame["id"].(string)
			if !hasID {
				continue
			}
			result := map[string]any{}
			switch method {
			case "initialize":
				result["userAgent"] = "codex-cli/1.2.3"
			case "config/read":
				result["config"] = capabilityProbeConfigFixture()
			case "thread/start":
				params, _ := frame["params"].(map[string]any)
				if params["modelProvider"] != capabilityProbeProviderID || params["model"] != capabilityProbeModel || params["ephemeral"] != true {
					serverDone <- &testCapabilityError{message: "thread/start did not carry typed isolated policy"}
					return
				}
				result = map[string]any{
					"modelProvider":   capabilityProbeProviderID,
					"model":           capabilityProbeModel,
					"reasoningEffort": capabilityProbeReasoning,
					"cwd":             "/tmp/codex-capability",
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

	observation, err := RunCapabilityPreflightSession(context.Background(), client, client, "test-version", "/tmp/codex-capability")
	if err != nil {
		t.Fatalf("RunCapabilityPreflightSession: %v", err)
	}
	if observation.CapabilitySet != CodexProfileCapabilitySetV1 || observation.UserAgent != "codex-cli/1.2.3" {
		t.Fatalf("unexpected observation: %#v", observation)
	}

	client.Close()
	if err := <-serverDone; err != nil && err != io.ErrClosedPipe {
		t.Fatalf("preflight server: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(methods, ","); got != "initialize,initialized,config/read,thread/start" {
		t.Fatalf("methods = %q", got)
	}
}

func TestRunCapabilityPreflightSessionFailsClosedOnContractMismatch(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		scanner := bufio.NewScanner(server)
		for scanner.Scan() {
			var frame map[string]any
			_ = json.Unmarshal(scanner.Bytes(), &frame)
			id, ok := frame["id"].(string)
			if !ok {
				continue
			}
			method, _ := frame["method"].(string)
			result := map[string]any{}
			if method == "config/read" {
				result["config"] = map[string]any{"model_provider": "wrong-provider"}
			}
			_ = writeProbeTestFrame(server, map[string]any{"id": id, "result": result})
		}
	}()

	_, err := RunCapabilityPreflightSession(context.Background(), client, client, "test-version", "/tmp/codex-capability")
	if got := OAuthProbeErrorCode(err); got != ErrorCodexCapabilityUnsupported {
		t.Fatalf("error code = %q, want %q (err=%v)", got, ErrorCodexCapabilityUnsupported, err)
	}
}

type testCapabilityError struct {
	message string
}

func (e *testCapabilityError) Error() string {
	return e.message
}
