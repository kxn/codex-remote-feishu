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

func TestRunOAuthProbeSessionUsesAuthOnlyProtocol(t *testing.T) {
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
				result["config"] = map[string]any{"chatgpt_base_url": OfficialChatGPTBaseURL}
			case "account/read":
				result["account"] = map[string]any{
					"type":     "chatgpt",
					"email":    "person@example.com",
					"planType": "team",
				}
				result["requiresOpenaiAuth"] = true
			case "getAuthStatus":
				result["authMethod"] = "chatgpt"
				result["authToken"] = "must-not-escape"
			}
			if err := writeProbeTestFrame(server, map[string]any{"id": id, "result": result}); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- scanner.Err()
	}()

	observation, err := RunOAuthProbeSession(context.Background(), client, client, "test-version")
	if err != nil {
		t.Fatalf("RunOAuthProbeSession: %v", err)
	}
	if observation.Result.Status != OAuthProbeStatusDetected || observation.Result.AccountHint != "p***@example.com" {
		t.Fatalf("unexpected observation: %#v", observation)
	}
	if observation.UserAgent != "codex-cli/1.2.3" || observation.CapabilitySet != OAuthProbeCapabilitySetV1 {
		t.Fatalf("unexpected capability evidence: %#v", observation)
	}
	raw, err := json.Marshal(observation)
	if err != nil {
		t.Fatalf("marshal observation: %v", err)
	}
	if strings.Contains(string(raw), "must-not-escape") || strings.Contains(string(raw), "person@example.com") {
		t.Fatalf("observation leaked auth response: %s", raw)
	}

	client.Close()
	if err := <-serverDone; err != nil && err != io.ErrClosedPipe {
		t.Fatalf("probe server: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(methods, ","); got != "initialize,initialized,config/read,account/read,getAuthStatus" {
		t.Fatalf("methods = %q", got)
	}
}

func TestRunOAuthProbeSessionFailsClosedWhenAuthCapabilityIsMissing(t *testing.T) {
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
			if method == "getAuthStatus" {
				_ = writeProbeTestFrame(server, map[string]any{
					"id":    id,
					"error": map[string]any{"code": -32601, "message": "Method not found"},
				})
				return
			}
			result := map[string]any{}
			if method == "account/read" {
				result["account"] = map[string]any{"type": "chatgpt"}
			}
			_ = writeProbeTestFrame(server, map[string]any{"id": id, "result": result})
		}
	}()

	_, err := RunOAuthProbeSession(context.Background(), client, client, "test-version")
	if got := OAuthProbeErrorCode(err); got != ErrorCodexCapabilityUnsupported {
		t.Fatalf("error code = %q, want %q (err=%v)", got, ErrorCodexCapabilityUnsupported, err)
	}
}

func TestRunOAuthProbeSessionClassifiesJSONRPCErrorsByCode(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		rpcCode int
		want    string
	}{
		{name: "initialize method missing", method: "initialize", rpcCode: -32601, want: ErrorCodexCapabilityUnsupported},
		{name: "config method missing", method: "config/read", rpcCode: -32601, want: ErrorCodexCapabilityUnsupported},
		{name: "account method missing", method: "account/read", rpcCode: -32601, want: ErrorCodexCapabilityUnsupported},
		{name: "auth status method missing", method: "getAuthStatus", rpcCode: -32601, want: ErrorCodexCapabilityUnsupported},
		{name: "auth status runtime failure", method: "getAuthStatus", rpcCode: -32000, want: ErrorOAuthProbeUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
					if method == test.method {
						_ = writeProbeTestFrame(server, map[string]any{
							"id":    id,
							"error": map[string]any{"code": test.rpcCode, "message": "probe failure"},
						})
						return
					}
					result := map[string]any{}
					if method == "config/read" {
						result["config"] = map[string]any{}
					}
					_ = writeProbeTestFrame(server, map[string]any{"id": id, "result": result})
				}
			}()

			_, err := RunOAuthProbeSession(context.Background(), client, client, "test-version")
			if got := OAuthProbeErrorCode(err); got != test.want {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.want, err)
			}
		})
	}
}

func TestOAuthProbeLaunchMaterialClearsConflictingAuthentication(t *testing.T) {
	baseEnv := []string{
		"HOME=/home/test",
		"CODEX_HOME=/home/test/.codex",
		"OPENAI_API_KEY=openai-secret",
		"CODEX_API_KEY=codex-secret",
		"CODEX_ACCESS_TOKEN=access-secret",
		"OPENAI_ORGANIZATION=org-secret",
		"OPENAI_PROJECT=project-secret",
		"CODEX_REFRESH_TOKEN_URL_OVERRIDE=https://refresh.invalid",
		"CODEX_REVOKE_TOKEN_URL_OVERRIDE=https://revoke.invalid",
		"CODEX_APP_SERVER_LOGIN_CLIENT_ID=client-secret",
		"CODEX_REMOTE_CODEX_PROVIDER_API_KEY=legacy-secret",
		"CODEX_REMOTE_CODEX_PROFILE_API_KEY=profile-secret",
	}

	material := OAuthProbeLaunchMaterial(baseEnv)
	if got := strings.Join(material.Args, "\x00"); got != strings.Join([]string{
		"app-server",
		"-c", `model_provider="openai"`,
		"-c", `openai_base_url=""`,
	}, "\x00") {
		t.Fatalf("args = %#v", material.Args)
	}
	for _, key := range ConflictingCodexAuthEnvKeys() {
		if lookupProbeTestEnv(material.Env, key) != "" {
			t.Fatalf("%s was not cleared from probe env", key)
		}
	}
	if lookupProbeTestEnv(material.Env, "CODEX_HOME") != "/home/test/.codex" {
		t.Fatalf("probe must preserve CODEX_HOME: %#v", material.Env)
	}
}

func writeProbeTestFrame(w io.Writer, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = w.Write(append(raw, '\n'))
	return err
}

func lookupProbeTestEnv(env []string, key string) string {
	for _, entry := range env {
		if current, value, ok := strings.Cut(entry, "="); ok && current == key {
			return value
		}
	}
	return ""
}
