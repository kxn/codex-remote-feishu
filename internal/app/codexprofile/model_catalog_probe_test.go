package codexprofile

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunModelCatalogProbeRealProcessIsReadOnlyAndPreservesNativeFields(t *testing.T) {
	report := filepath.Join(t.TempDir(), "report.json")
	options := ModelCatalogProbeOptions{BinaryPath: os.Args[0], Env: append(os.Environ(), "GO_WANT_CODEX_OAUTH_PROBE_HELPER=model-catalog", "MODEL_CATALOG_REPORT="+report, "MODEL_CATALOG_RESOLVED_AUTH=fixture-private-auth"), Args: []string{"app-server", "--listen=stdio://", "-c", `model_provider="openai"`}, Version: "test", Cursor: "page-two", Limit: 7}
	observation, err := RunModelCatalogProbe(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := observation.Snapshot
	if observation.EvidenceID == "" || snapshot.RefreshedAt.IsZero() || snapshot.NextCursor != "native-next" || len(snapshot.Entries) != 1 {
		t.Fatalf("missing exact native page: %+v", observation)
	}
	entry := snapshot.Entries[0]
	if entry.Model != "native-model" || !entry.IsDefault || entry.DefaultReasoningEffort != "high" || len(entry.SupportedReasoningEfforts) != 2 || entry.SupportedReasoningEfforts[1].ReasoningEffort != "xhigh" || entry.DisplayName != "Native model" {
		t.Fatalf("parser lost model fields: %+v", entry)
	}
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		CWD           string
		Methods       []string
		Cursor        string
		Limit         int
		Private, Auth bool
	}
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Methods, []string{"initialize", "initialized", "model/list"}) || got.Cursor != "page-two" || got.Limit != 7 || !got.Private || !got.Auth {
		t.Fatalf("unexpected subprocess protocol: %+v", got)
	}
	if _, err = os.Stat(got.CWD); !os.IsNotExist(err) {
		t.Fatal("disposable working directory was not removed")
	}
}

func TestRunModelCatalogProbeRejectsErrorsAndCancelsProcess(t *testing.T) {
	for _, mode := range []string{"model-catalog-rejected", "model-catalog-invalid", "model-catalog-timeout"} {
		t.Run(mode, func(t *testing.T) {
			report := filepath.Join(t.TempDir(), "report.json")
			timeout := 3 * time.Second
			if mode == "model-catalog-timeout" {
				timeout = 150 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			start := time.Now()
			_, err := RunModelCatalogProbe(ctx, ModelCatalogProbeOptions{BinaryPath: os.Args[0], Env: append(os.Environ(), "GO_WANT_CODEX_OAUTH_PROBE_HELPER="+mode, "MODEL_CATALOG_REPORT="+report)})
			if err == nil {
				t.Fatal("invalid model probe succeeded")
			}
			if strings.Contains(err.Error(), "sensitive-server-error") {
				t.Fatal("native rejection leaked raw error")
			}
			if mode == "model-catalog-timeout" && (OAuthProbeErrorCode(err) != ErrorCodexProbeTimeout || time.Since(start) > 2*time.Second) {
				t.Fatalf("unbounded timeout: %v", err)
			}
			raw, e := os.ReadFile(report)
			if e != nil {
				t.Fatal(e)
			}
			var got struct{ CWD string }
			if e = json.Unmarshal(raw, &got); e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(got.CWD); !os.IsNotExist(e) {
				t.Fatal("failed process working directory leaked")
			}
		})
	}
}

func runModelCatalogProbeHelper(mode string) int {
	cwd, e := os.Getwd()
	if e != nil {
		return 10
	}
	report := struct {
		CWD           string
		Methods       []string
		Cursor        string
		Limit         int
		Private, Auth bool
	}{CWD: cwd, Private: os.Getenv("CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED") == "1", Auth: os.Getenv("MODEL_CATALOG_RESOLVED_AUTH") == "fixture-private-auth"}
	save := func() { raw, _ := json.Marshal(report); _ = os.WriteFile(os.Getenv("MODEL_CATALOG_REPORT"), raw, 0600) }
	save()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var frame struct {
			ID     string
			Method string
			Params struct {
				Cursor string
				Limit  int
			}
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			return 11
		}
		report.Methods = append(report.Methods, frame.Method)
		save()
		switch frame.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", frame.ID)
		case "initialized":
		case "account/read":
			fmt.Printf(`{"id":%q,"result":{"account":{"type":"chatgpt","email":"user-one@example.com","planType":"plus"}}}`+"\n", frame.ID)
		case "model/list":
			report.Cursor, report.Limit = frame.Params.Cursor, frame.Params.Limit
			save()
			if mode == "model-catalog-timeout" {
				time.Sleep(10 * time.Minute)
				return 12
			}
			if mode == "model-catalog-rejected" {
				fmt.Printf(`{"id":%q,"error":{"code":-32601,"message":"sensitive-server-error"}}`+"\n", frame.ID)
				continue
			}
			if mode == "model-catalog-invalid" {
				fmt.Printf(`{"id":%q,"result":{}}`+"\n", frame.ID)
				continue
			}
			fmt.Printf(`{"id":%q,"result":{"data":[{"id":"native-model","model":"native-model","displayName":"Native model","isDefault":true,"defaultReasoningEffort":"high","supportedReasoningEfforts":[{"reasoningEffort":"high"},{"reasoningEffort":"xhigh"}]}],"nextCursor":"native-next"}}`+"\n", frame.ID)
		default:
			return 13 // No thread, prompt, config mutation, or tool command is allowed.
		}
	}
	return 0
}

func TestRunModelCatalogProbeChecksAccountInSameProcessBeforeListing(t *testing.T) {
	for _, tc := range []struct {
		name, email, plan string
		success           bool
	}{{"same", "user-one@example.com", "plus", true}, {"same-redacted-different-account", "user-two@example.com", "plus", false}, {"plan-changed", "user-one@example.com", "pro", false}} {
		t.Run(tc.name, func(t *testing.T) {
			report := filepath.Join(t.TempDir(), "report.json")
			expected := ClassifyOAuthProbe(OAuthProbeEvidence{AccountType: "chatgpt", AuthMethod: "chatgpt", Email: tc.email})
			result, err := RunModelCatalogProbe(t.Context(), ModelCatalogProbeOptions{BinaryPath: os.Args[0], Env: append(os.Environ(), "GO_WANT_CODEX_OAUTH_PROBE_HELPER=model-catalog", "MODEL_CATALOG_REPORT="+report), ExpectedAccountFingerprint: expected.AccountFingerprint, ExpectedPlanType: tc.plan})
			if tc.success {
				if err != nil || result.AccountFingerprint != expected.AccountFingerprint || result.PlanType != "plus" {
					t.Fatalf("same-process proof failed: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("different native account/plan accepted")
			}
			raw, e := os.ReadFile(report)
			if e != nil {
				t.Fatal(e)
			}
			var got struct{ Methods []string }
			if e = json.Unmarshal(raw, &got); e != nil {
				t.Fatal(e)
			}
			want := []string{"initialize", "initialized", "account/read"}
			if tc.success {
				want = append(want, "model/list")
			}
			if !reflect.DeepEqual(got.Methods, want) {
				t.Fatalf("account verification occurred after listing: %v", got.Methods)
			}
		})
	}
}
