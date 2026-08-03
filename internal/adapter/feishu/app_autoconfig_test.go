package feishu

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"

	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

func TestV6AppAutoConfigReadRequestsIncludeRequiredLang(t *testing.T) {
	appID := "cli_v6_lang"
	versionID := "oav_v6_lang"
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
		case "/open-apis/application/v6/applications/" + appID:
			paths = append(paths, r.URL.String())
			if got := r.URL.Query().Get("lang"); got != "zh_cn" {
				t.Fatalf("application.get lang = %q, want zh_cn", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"app":{"app_id":"cli_v6_lang","online_version_id":"oav_v6_lang"}}}`))
		case "/open-apis/application/v6/applications/" + appID + "/app_versions/" + versionID:
			paths = append(paths, r.URL.String())
			if got := r.URL.Query().Get("lang"); got != "zh_cn" {
				t.Fatalf("application_app_version.get lang = %q, want zh_cn", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"app_version":{"app_id":"cli_v6_lang","version_id":"oav_v6_lang","version":"1.0.0","status":1}}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	setup := NewSetupClient(SetupClientConfig{
		GatewayID: "main",
		AppID:     appID,
		AppSecret: "secret",
		Domain:    server.URL,
	})
	client, broker := setup.sdk()
	app, err := getApplicationConfig(context.Background(), broker, client, appID)
	if err != nil {
		t.Fatalf("getApplicationConfig: %v", err)
	}
	if app == nil || stringValue(app.OnlineVersionId) != versionID {
		t.Fatalf("unexpected app response: %#v", app)
	}
	version, err := getApplicationVersion(context.Background(), broker, client, appID, versionID)
	if err != nil {
		t.Fatalf("getApplicationVersion: %v", err)
	}
	if version == nil || stringValue(version.VersionId) != versionID {
		t.Fatalf("unexpected version response: %#v", version)
	}
	if len(paths) != 2 {
		t.Fatalf("read request count = %d, want 2 paths=%#v", len(paths), paths)
	}
}

func TestPlanAppAutoConfigReportsDiffAndRequirementState(t *testing.T) {
	restoreAutoConfigHooks(t)
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
			},
			Event: &larkapplication.SubscribedEvent{
				SubscriptionType: strp("webhook"),
				RequestUrl:       strp("https://legacy.example.com"),
			},
			Callback: &larkapplication.Callback{
				CallbackType: strp("websocket"),
			},
			OnlineVersionId: strp("online-1"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp("online-1"),
			Version:   strp("1.0.0"),
			Status:    intp(larkapplication.AppVersionStatusAudited),
		}, nil
	}

	plan, err := PlanAppAutoConfig(context.Background(), LiveGatewayConfig{GatewayID: "main", AppID: "cli_xxx"}, testAutoConfigManifest(), feishuapp.DefaultFixedPolicy())
	if err != nil {
		t.Fatalf("PlanAppAutoConfig: %v", err)
	}
	if plan.Status != AutoConfigStatusApplyRequired {
		t.Fatalf("plan status = %q, want %q", plan.Status, AutoConfigStatusApplyRequired)
	}
	if !plan.Diff.ConfigPatchRequired || !plan.Diff.AbilityPatchRequired {
		t.Fatalf("expected config+ability patch required, got %#v", plan.Diff)
	}
	if !reflect.DeepEqual(plan.Diff.MissingEvents, []string{"im.message.receive_v1"}) {
		t.Fatalf("missing events = %#v", plan.Diff.MissingEvents)
	}
	if len(plan.BlockingRequirements) != 2 {
		t.Fatalf("blocking requirements = %#v", plan.BlockingRequirements)
	}
	if len(plan.DegradableRequirements) != 0 {
		t.Fatalf("degradable requirements = %#v", plan.DegradableRequirements)
	}
}

func TestPlanAppAutoConfigRequirementPresenceUsesConfiguredScopes(t *testing.T) {
	restoreAutoConfigHooks(t)
	// Presence of a scope must come from the app config (configured scopes),
	// not from the tenant authorization query (scope.list). The plan read path
	// must not consult scope.list at all.
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
			Event: &larkapplication.SubscribedEvent{
				SubscriptionType: strp("websocket"),
				RequestUrl:       strp(""),
				SubscribedEvents: []string{"im.message.receive_v1"},
			},
			Callback: &larkapplication.Callback{
				CallbackType:        strp("websocket"),
				SubscribedCallbacks: []string{"card.action.trigger"},
			},
			OnlineVersionId: strp("online-1"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp("online-1"),
			Version:   strp("1.0.0"),
			Status:    intp(larkapplication.AppVersionStatusAudited),
			Ability:   &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
			Events:    []string{"im.message.receive_v1"},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_configured"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if plan.Status != AutoConfigStatusClean {
		t.Fatalf("plan status = %q, want %q (diff=%#v blocking=%#v)", plan.Status, AutoConfigStatusClean, plan.Diff, plan.BlockingRequirements)
	}
	if len(plan.BlockingRequirements) != 0 || len(plan.DegradableRequirements) != 0 {
		t.Fatalf("expected no missing requirements, got blocking=%#v degradable=%#v", plan.BlockingRequirements, plan.DegradableRequirements)
	}
}

func TestPlanAppAutoConfigClassifiesReadAPIErrorWithoutRawUserText(t *testing.T) {
	restoreAutoConfigHooks(t)
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return nil, &APIError{
			API:        "application.v6.application.get",
			Code:       99992402,
			Msg:        "field validation failed",
			StatusCode: http.StatusBadRequest,
		}
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_invalid"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if plan.Status != AutoConfigStatusBlocked {
		t.Fatalf("plan status = %q, want %q", plan.Status, AutoConfigStatusBlocked)
	}
	if plan.BlockingReason != autoConfigBlockingReadFailed {
		t.Fatalf("blocking reason = %q, want %q", plan.BlockingReason, autoConfigBlockingReadFailed)
	}
	raw := plan.Summary + " " + plan.BlockingReason
	for _, disallowed := range []string{"application.v6.application.get", "99992402", "field validation failed", "feishu_api_error"} {
		if strings.Contains(raw, disallowed) {
			t.Fatalf("plan leaked raw error token %q in %q", disallowed, raw)
		}
	}
}

func TestOverridePlanFromAPIErrorClassifiesStableFailureReasons(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus string
		wantReason string
	}{
		{
			name:       "permission denied",
			err:        &APIError{API: "application.v6.application.get", Code: 99991663, Msg: "permission denied", StatusCode: http.StatusForbidden},
			wantStatus: AutoConfigStatusBlocked,
			wantReason: autoConfigBlockingPermissionIssue,
		},
		{
			name:       "unknown read error",
			err:        errors.New("network read reset"),
			wantStatus: AutoConfigStatusBlocked,
			wantReason: autoConfigBlockingReadFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := overridePlanFromAPIError(AutoConfigPlan{}, tt.err)
			if plan.Status != tt.wantStatus || plan.BlockingReason != tt.wantReason {
				t.Fatalf("overridePlanFromAPIError = status %q reason %q, want %q/%q", plan.Status, plan.BlockingReason, tt.wantStatus, tt.wantReason)
			}
			for _, disallowed := range []string{"field validation failed", "permission denied", "network read reset", "feishu_api_error"} {
				if strings.Contains(plan.Summary, disallowed) || strings.Contains(plan.BlockingReason, disallowed) {
					t.Fatalf("classified user text leaked %q in summary=%q reason=%q", disallowed, plan.Summary, plan.BlockingReason)
				}
			}
		})
	}
}

func restoreAutoConfigHooks(t *testing.T) {
	t.Helper()
	oldGetApp := autoConfigGetApplication
	oldGetVersion := autoConfigGetApplicationVersion
	t.Cleanup(func() {
		autoConfigGetApplication = oldGetApp
		autoConfigGetApplicationVersion = oldGetVersion
	})
}

func testAutoConfigManifest() feishuapp.Manifest {
	return feishuapp.Manifest{
		Scopes: feishuapp.ScopesImport{
			Scopes: feishuapp.PermissionScopes{
				Tenant: []string{"im:message", "drive:drive"},
			},
		},
		ScopeRequirements: []feishuapp.ScopeRequirement{
			{Scope: "im:message", ScopeType: "tenant", Feature: "core", Required: true},
			{Scope: "drive:drive", ScopeType: "tenant", Feature: "preview", Required: true},
		},
		Events: []feishuapp.EventRequirement{
			{Event: "im.message.receive_v1", Feature: "core", Required: true},
		},
		Callbacks: []feishuapp.CallbackRequirement{
			{Callback: "card.action.trigger", Feature: "cards", Required: true},
		},
	}
}

func strp(value string) *string {
	return &value
}

func intp(value int) *int {
	return &value
}
