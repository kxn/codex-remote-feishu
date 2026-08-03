package feishu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
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

func TestCompleteAppAutoConfigSendsDocumentedV7RequestBodies(t *testing.T) {
	appID := "cli_complete_contract"
	phase := 0
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
		case "/open-apis/application/v6/applications/" + appID:
			calls = append(calls, "get-app")
			if got := r.URL.Query().Get("lang"); got != "zh_cn" {
				t.Fatalf("application.get lang = %q, want zh_cn", got)
			}
			if phase == 0 {
				_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"app":{"app_id":"cli_complete_contract"}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"app":{"app_id":"cli_complete_contract","unaudit_version_id":"draft-1","scopes":[{"scope":"im:message","token_types":["tenant"]},{"scope":"drive:drive","token_types":["tenant"]}],"event":{"subscription_type":"websocket","subscribed_events":["im.message.receive_v1"]},"callback":{"callback_type":"websocket","subscribed_callbacks":["card.action.trigger"]}}}}`))
		case "/open-apis/application/v6/scopes":
			calls = append(calls, "list-scopes")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"scopes":[{"scope_name":"im:message","scope_type":"tenant","grant_status":1},{"scope_name":"drive:drive","scope_type":"tenant","grant_status":1}]}}`))
		case "/open-apis/application/v6/applications/" + appID + "/app_versions/draft-1":
			calls = append(calls, "get-version")
			if got := r.URL.Query().Get("lang"); got != "zh_cn" {
				t.Fatalf("application_app_version.get lang = %q, want zh_cn", got)
			}
			status := larkapplication.AppVersionStatusUnaudit
			if phase >= 2 {
				status = larkapplication.AppVersionStatusUnderAudit
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"app_version":{"app_id":"cli_complete_contract","version_id":"draft-1","version":"1.0.1","status":` + strconv.Itoa(status) + `,"ability":{"bot":{}}}}}`))
		case "/open-apis/application/v7/applications/" + appID + "/ability":
			calls = append(calls, "patch-ability")
			body := requireAutoConfigHTTPBody(t, r, http.MethodPatch)
			requireBodyContains(t, body, `"bot":{"enable":true}`)
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
		case "/open-apis/application/v7/applications/" + appID + "/config":
			calls = append(calls, "patch-config")
			body := requireAutoConfigHTTPBody(t, r, http.MethodPatch)
			requireBodyContains(t, body, `"scope_name":"im:message"`, `"scope_name":"drive:drive"`, `"subscription_type":"websocket"`, `"callback_type":"websocket"`)
			if strings.Contains(body, `"request_url"`) {
				t.Fatalf("websocket config patch should omit empty request_url, got %s", body)
			}
			phase = 1
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
		case "/open-apis/application/v7/applications/" + appID + "/publish":
			calls = append(calls, "publish")
			body := requireAutoConfigHTTPBody(t, r, http.MethodPost)
			requireBodyContains(t, body, `"mobile_default_ability":"bot"`, `"pc_default_ability":"bot"`, `"remark":"同步 Codex Remote 的飞书应用配置"`, `"changelog":"更新飞书自动配置所需的权限、事件、回调与机器人能力。"`)
			phase = 2
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"version_id":"draft-1","version":"1.0.1"}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	result, err := CompleteAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: appID, AppSecret: "secret", Domain: server.URL},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
		AutoConfigPublishRequest{},
	)
	if err != nil {
		t.Fatalf("CompleteAppAutoConfig: %v", err)
	}
	if result.Status != AutoConfigStatusAwaitingReview {
		t.Fatalf("complete status = %q, want %q", result.Status, AutoConfigStatusAwaitingReview)
	}
	if !reflect.DeepEqual(result.Actions, []AutoConfigAction{
		{Name: "ability_patch", Outcome: "applied"},
		{Name: "config_patch", Outcome: "applied"},
		{Name: "publish", Outcome: "submitted"},
	}) {
		t.Fatalf("complete actions = %#v", result.Actions)
	}
	wantCalls := []string{
		"get-app", "list-scopes",
		"get-app", "list-scopes",
		"patch-ability", "patch-config",
		"get-app", "list-scopes", "get-version",
		"get-app", "list-scopes", "get-version",
		"publish",
		"get-app", "list-scopes", "get-version",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestPlanAppAutoConfigReportsDiffAndRequirementState(t *testing.T) {
	restoreAutoConfigHooks(t)
	autoConfigListScopes = func(*SetupClient, context.Context) ([]AppScopeStatus, error) {
		return []AppScopeStatus{{ScopeName: "im:message", ScopeType: "tenant", GrantStatus: 1}}, nil
	}
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
	if !plan.Diff.EventSubscriptionTypeMismatch || !plan.Diff.EventRequestURLMismatch {
		t.Fatalf("expected event policy mismatch, got %#v", plan.Diff)
	}
	if !reflect.DeepEqual(plan.Diff.MissingEvents, []string{"im.message.receive_v1"}) {
		t.Fatalf("missing events = %#v", plan.Diff.MissingEvents)
	}
	if !reflect.DeepEqual(plan.Diff.MissingCallbacks, []string{"card.action.trigger"}) {
		t.Fatalf("missing callbacks = %#v", plan.Diff.MissingCallbacks)
	}
	if len(plan.BlockingRequirements) != 3 {
		t.Fatalf("blocking requirements = %#v", plan.BlockingRequirements)
	}
	if len(plan.DegradableRequirements) != 0 {
		t.Fatalf("degradable requirements = %#v", plan.DegradableRequirements)
	}
}

func TestApplyAppAutoConfigEnablesBotBeforeConfigPatch(t *testing.T) {
	restoreAutoConfigHooks(t)
	phase := 0
	var calls []string
	autoConfigListScopes = func(*SetupClient, context.Context) ([]AppScopeStatus, error) {
		return nil, nil
	}
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		if phase == 0 {
			return &larkapplication.Application{}, nil
		}
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
				RequestUrl:          strp(""),
				SubscribedCallbacks: []string{"card.action.trigger"},
			},
			UnauditVersionId: strp("draft-1"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		if phase == 0 {
			return nil, nil
		}
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp("draft-1"),
			Version:   strp("1.0.1"),
			Status:    intp(larkapplication.AppVersionStatusUnaudit),
			Ability: &larkapplication.AppAbility{
				Bot: &larkapplication.Bot{},
			},
		}, nil
	}
	autoConfigPatchAbility = func(context.Context, *FeishuCallBroker, *lark.Client, string, v7PatchAbilityRequest) error {
		calls = append(calls, "ability")
		return nil
	}
	autoConfigPatchConfig = func(context.Context, *FeishuCallBroker, *lark.Client, string, v7PatchConfigRequest) error {
		calls = append(calls, "config")
		phase = 1
		return nil
	}

	result, err := ApplyAppAutoConfig(context.Background(), LiveGatewayConfig{GatewayID: "main", AppID: "cli_xxx"}, testAutoConfigManifest(), feishuapp.DefaultFixedPolicy())
	if err != nil {
		t.Fatalf("ApplyAppAutoConfig: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"ability", "config"}) {
		t.Fatalf("patch order = %#v, want ability then config", calls)
	}
	if result.Status != AutoConfigStatusPublishRequired {
		t.Fatalf("apply status = %q, want %q", result.Status, AutoConfigStatusPublishRequired)
	}
	if !result.Plan.Publish.NeedsPublish {
		t.Fatalf("expected publish to be required after apply, got %#v", result.Plan.Publish)
	}
}

func TestPublishAppAutoConfigBlocksUntilApplyCompletes(t *testing.T) {
	restoreAutoConfigHooks(t)
	calledPublish := false
	autoConfigListScopes = func(*SetupClient, context.Context) ([]AppScopeStatus, error) {
		return nil, nil
	}
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		return nil, nil
	}
	autoConfigPublish = func(context.Context, *FeishuCallBroker, *lark.Client, string, v7PublishRequest) (string, string, error) {
		calledPublish = true
		return "", "", nil
	}

	result, err := PublishAppAutoConfig(context.Background(), LiveGatewayConfig{GatewayID: "main", AppID: "cli_xxx"}, testAutoConfigManifest(), feishuapp.DefaultFixedPolicy(), AutoConfigPublishRequest{})
	if err != nil {
		t.Fatalf("PublishAppAutoConfig: %v", err)
	}
	if calledPublish {
		t.Fatal("publish call should not happen while apply is still required")
	}
	if result.Status != AutoConfigStatusBlocked || result.BlockingReason != autoConfigBlockingApplyRequired {
		t.Fatalf("unexpected publish result: %#v", result)
	}
}

func TestCompleteAppAutoConfigAppliesThenPublishes(t *testing.T) {
	restoreAutoConfigHooks(t)
	phase := 0
	var calls []string
	autoConfigListScopes = func(*SetupClient, context.Context) ([]AppScopeStatus, error) {
		return nil, nil
	}
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		if phase == 0 {
			return &larkapplication.Application{}, nil
		}
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
			Event: &larkapplication.SubscribedEvent{
				SubscriptionType: strp("websocket"),
				SubscribedEvents: []string{"im.message.receive_v1"},
			},
			Callback: &larkapplication.Callback{
				CallbackType:        strp("websocket"),
				SubscribedCallbacks: []string{"card.action.trigger"},
			},
			UnauditVersionId: strp("draft-1"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		if phase == 0 {
			return nil, nil
		}
		status := larkapplication.AppVersionStatusUnaudit
		if phase >= 2 {
			status = larkapplication.AppVersionStatusUnderAudit
		}
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp("draft-1"),
			Version:   strp("1.0.1"),
			Status:    intp(status),
			Ability: &larkapplication.AppAbility{
				Bot: &larkapplication.Bot{},
			},
		}, nil
	}
	autoConfigPatchAbility = func(context.Context, *FeishuCallBroker, *lark.Client, string, v7PatchAbilityRequest) error {
		calls = append(calls, "ability")
		return nil
	}
	autoConfigPatchConfig = func(context.Context, *FeishuCallBroker, *lark.Client, string, v7PatchConfigRequest) error {
		calls = append(calls, "config")
		phase = 1
		return nil
	}
	autoConfigPublish = func(context.Context, *FeishuCallBroker, *lark.Client, string, v7PublishRequest) (string, string, error) {
		calls = append(calls, "publish")
		phase = 2
		return "draft-1", "1.0.1", nil
	}

	result, err := CompleteAppAutoConfig(context.Background(), LiveGatewayConfig{GatewayID: "main", AppID: "cli_xxx"}, testAutoConfigManifest(), feishuapp.DefaultFixedPolicy(), AutoConfigPublishRequest{})
	if err != nil {
		t.Fatalf("CompleteAppAutoConfig: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"ability", "config", "publish"}) {
		t.Fatalf("complete calls = %#v, want ability, config, publish", calls)
	}
	if result.Status != AutoConfigStatusAwaitingReview {
		t.Fatalf("complete status = %q, want %q", result.Status, AutoConfigStatusAwaitingReview)
	}
	if result.VersionID != "draft-1" || result.Version != "1.0.1" {
		t.Fatalf("complete publish version = %q/%q", result.VersionID, result.Version)
	}
}

func TestApplyAppAutoConfigKeepsActionEvidenceWhenFinalVerificationFails(t *testing.T) {
	restoreAutoConfigHooks(t)
	phase := 0
	autoConfigListScopes = func(*SetupClient, context.Context) ([]AppScopeStatus, error) {
		return nil, nil
	}
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		if phase > 0 {
			return nil, errors.New("final plan read failed")
		}
		return &larkapplication.Application{}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		return nil, nil
	}
	autoConfigPatchAbility = func(context.Context, *FeishuCallBroker, *lark.Client, string, v7PatchAbilityRequest) error {
		return nil
	}
	autoConfigPatchConfig = func(context.Context, *FeishuCallBroker, *lark.Client, string, v7PatchConfigRequest) error {
		phase = 1
		return nil
	}

	result, err := ApplyAppAutoConfig(context.Background(), LiveGatewayConfig{GatewayID: "main", AppID: "cli_xxx"}, testAutoConfigManifest(), feishuapp.DefaultFixedPolicy())
	if err != nil {
		t.Fatalf("ApplyAppAutoConfig: %v", err)
	}
	if result.Status != AutoConfigStatusVerificationFailed {
		t.Fatalf("apply status = %q, want %q", result.Status, AutoConfigStatusVerificationFailed)
	}
	if result.VerificationStatus != AutoConfigVerificationStatusFailed || result.VerificationError == "" {
		t.Fatalf("expected verification failure details, got %#v", result)
	}
	if !reflect.DeepEqual(result.Actions, []AutoConfigAction{
		{Name: "ability_patch", Outcome: "applied"},
		{Name: "config_patch", Outcome: "applied"},
	}) {
		t.Fatalf("expected applied action evidence, got %#v", result.Actions)
	}
}

func TestPublishAppAutoConfigKeepsSubmittedActionWhenFinalVerificationFails(t *testing.T) {
	restoreAutoConfigHooks(t)
	phase := 0
	autoConfigListScopes = func(*SetupClient, context.Context) ([]AppScopeStatus, error) {
		return []AppScopeStatus{
			{ScopeName: "im:message", ScopeType: "tenant", GrantStatus: 1},
			{ScopeName: "drive:drive", ScopeType: "tenant", GrantStatus: 1},
		}, nil
	}
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		if phase > 0 {
			return nil, errors.New("final plan read failed")
		}
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
			Event: &larkapplication.SubscribedEvent{
				SubscriptionType: strp("websocket"),
				SubscribedEvents: []string{"im.message.receive_v1"},
			},
			Callback: &larkapplication.Callback{
				CallbackType:        strp("websocket"),
				SubscribedCallbacks: []string{"card.action.trigger"},
			},
			UnauditVersionId: strp("draft-1"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp("draft-1"),
			Version:   strp("1.0.1"),
			Status:    intp(larkapplication.AppVersionStatusUnaudit),
			Ability: &larkapplication.AppAbility{
				Bot: &larkapplication.Bot{},
			},
		}, nil
	}
	autoConfigPublish = func(context.Context, *FeishuCallBroker, *lark.Client, string, v7PublishRequest) (string, string, error) {
		phase = 1
		return "draft-1", "1.0.1", nil
	}

	result, err := PublishAppAutoConfig(context.Background(), LiveGatewayConfig{GatewayID: "main", AppID: "cli_xxx"}, testAutoConfigManifest(), feishuapp.DefaultFixedPolicy(), AutoConfigPublishRequest{})
	if err != nil {
		t.Fatalf("PublishAppAutoConfig: %v", err)
	}
	if result.Status != AutoConfigStatusVerificationFailed {
		t.Fatalf("publish status = %q, want %q", result.Status, AutoConfigStatusVerificationFailed)
	}
	if result.VersionID != "draft-1" || result.Version != "1.0.1" {
		t.Fatalf("expected publish version evidence, got %q/%q", result.VersionID, result.Version)
	}
	if result.VerificationStatus != AutoConfigVerificationStatusFailed || result.VerificationError == "" {
		t.Fatalf("expected verification failure details, got %#v", result)
	}
	if !reflect.DeepEqual(result.Actions, []AutoConfigAction{{Name: "publish", Outcome: "submitted"}}) {
		t.Fatalf("expected submitted action evidence, got %#v", result.Actions)
	}
}

func TestPlanAppAutoConfigReturnsUnsupportedPlanInsteadOfError(t *testing.T) {
	restoreAutoConfigHooks(t)
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return nil, &APIError{
			API:  "application.v6.application.get",
			Code: 210015,
			Msg:  "unsupported application",
		}
	}
	autoConfigListScopes = func(*SetupClient, context.Context) ([]AppScopeStatus, error) {
		return nil, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_legacy"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if plan.Status != AutoConfigStatusUnsupported {
		t.Fatalf("plan status = %q, want %q", plan.Status, AutoConfigStatusUnsupported)
	}
	if plan.BlockingReason != autoConfigBlockingUnsupported {
		t.Fatalf("blocking reason = %q, want %q", plan.BlockingReason, autoConfigBlockingUnsupported)
	}
}

func restoreAutoConfigHooks(t *testing.T) {
	t.Helper()
	oldListScopes := autoConfigListScopes
	oldGetApp := autoConfigGetApplication
	oldGetVersion := autoConfigGetApplicationVersion
	oldPatchConfig := autoConfigPatchConfig
	oldPatchAbility := autoConfigPatchAbility
	oldPublish := autoConfigPublish
	t.Cleanup(func() {
		autoConfigListScopes = oldListScopes
		autoConfigGetApplication = oldGetApp
		autoConfigGetApplicationVersion = oldGetVersion
		autoConfigPatchConfig = oldPatchConfig
		autoConfigPatchAbility = oldPatchAbility
		autoConfigPublish = oldPublish
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

func requireAutoConfigHTTPBody(t *testing.T, r *http.Request, method string) string {
	t.Helper()
	if r.Method != method {
		t.Fatalf("method = %s, want %s", r.Method, method)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
		t.Fatalf("Authorization = %q, want tenant token", got)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	return string(body)
}
