package teamcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func TestHelpAndDiscoveryNeedNoCredential(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"commands"}, {"task.assign", "--help"}, {"help", "project.create"}} {
		var out bytes.Buffer
		err := Run(context.Background(), Options{Args: args, Stdout: &out, Getenv: func(string) string { t.Error("help accessed credential environment"); return "" }})
		if err != nil || !json.Valid(out.Bytes()) {
			t.Fatalf("help %v: %v %s", args, err, out.String())
		}
	}
}

func TestCLIForwardsFileOrStdinWithExactRunAndKeepsAcceptedStatus(t *testing.T) {
	for _, mode := range []string{"file", "stdin", "dash"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != workcmd.ExecutePath || r.Header.Get("Authorization") != "Bearer secret-token" {
					t.Errorf("request %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
				}
				var req workcmd.DispatchRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if req.Name != "task.assign" || req.RunRef != "run-exact" || !bytes.Contains(req.Payload, []byte(`"key":"assign-one"`)) {
					t.Errorf("request %#v", req)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"result":{"status":"accepted","operation_id":"op-one","revision":3,"replay":false}}`)
			}))
			defer server.Close()
			body := `{"key":"assign-one","title":"Task","owner_role_id":"expert","requirements":[]}`
			args := []string{"task", "assign", "--run-ref", "run-exact"}
			switch mode {
			case "file":
				path := filepath.Join(t.TempDir(), "payload.json")
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--file", path)
			case "stdin":
				args = append(args, "--stdin")
			case "dash":
				args = append(args, "--file", "-")
			}
			var out bytes.Buffer
			err := Run(context.Background(), Options{Args: args, Stdin: strings.NewReader(body), Stdout: &out, Getenv: func(key string) string {
				switch key {
				case config.WorkToolEndpointEnv:
					return server.URL
				case config.WorkSessionTokenEnv:
					return "secret-token"
				}
				return ""
			}})
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || result["status"] != "accepted" || strings.Contains(out.String(), "secret-token") {
				t.Fatalf("result %s calls=%d", out.String(), calls)
			}
		})
	}
}

func TestCLIArgumentAndPayloadErrorsNeverSendRequests(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	getenv := func(key string) string {
		if key == config.WorkToolEndpointEnv {
			return server.URL
		}
		if key == config.WorkSessionTokenEnv {
			return "secret-token"
		}
		return ""
	}
	cases := []struct {
		name string
		args []string
		body string
	}{
		{"missing-run", []string{"context"}, ""},
		{"no-latest-fallback", []string{"context", "--latest"}, ""},
		{"unknown-flag", []string{"context", "--run-ref", "run", "--typo"}, ""},
		{"duplicate-flag", []string{"context", "--run-ref", "run", "--run-ref", "other"}, ""},
		{"missing-flag-value", []string{"context", "--run-ref"}, ""},
		{"extra-positional", []string{"context", "--run-ref", "run", "oops"}, ""},
		{"missing-payload", []string{"task.assign", "--run-ref", "run"}, ""},
		{"conflicting-inputs", []string{"task.assign", "--run-ref", "run", "--file", "-", "--stdin"}, "{}"},
		{"bad-json", []string{"task.assign", "--run-ref", "run", "--stdin"}, "{"},
		{"duplicate-json", []string{"task.assign", "--run-ref", "run", "--stdin"}, `{"key":"one","key":"two"}`},
		{"context-fields", []string{"context", "--run-ref", "run", "--stdin"}, `{"scope_id":"other"}`},
		{"too-large", []string{"task.assign", "--run-ref", "run", "--stdin"}, strings.Repeat("x", workcmd.MaxRequestBytes+1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := Run(context.Background(), Options{Args: test.args, Stdin: strings.NewReader(test.body), Stdout: io.Discard, Getenv: getenv})
			if err == nil {
				t.Error("invalid request accepted")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid requests reached network: %d", calls)
	}
}

func TestBootstrapCreationAloneMayOmitRunRef(t *testing.T) {
	if _, err := parse([]string{"project.create", "--stdin"}); err != nil {
		t.Fatal(err)
	}
	request := workcmd.DispatchRequest{Name: "project.create", Payload: json.RawMessage(`{"key":"bootstrap-operation"}`)}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	request.Name = "task.assign"
	if err := request.Validate(); err == nil {
		t.Error("worker command omitted run_ref")
	}
}
