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
			CallbackInfo: &larkapplication.CallbackInfo{
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
			Events:    []string{"some.other.event_v1"},
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

func TestPlanAppAutoConfigEventsSkippedWhenNoVersionReadable(t *testing.T) {
	restoreAutoConfigHooks(t)
	// Scan-created agent apps can be configured without a readable v6 version
	// (application.get may return no version IDs and the version list may be
	// empty). Events must not be reported missing in that state.
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
		}, nil
	}
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return nil, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_no_version"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if len(plan.Diff.MissingEvents) != 0 {
		t.Fatalf("missing events = %#v, want none when no version is readable", plan.Diff.MissingEvents)
	}
	for _, item := range plan.BlockingRequirements {
		if item.Kind == AutoConfigRequirementKindEvent {
			t.Fatalf("event %q must not be a blocking requirement when no version is readable", item.Key)
		}
	}
}

func TestPlanAppAutoConfigEventsReadFromPublishedVersionListFallback(t *testing.T) {
	restoreAutoConfigHooks(t)
	// When application.get returns no version IDs, the version list endpoint
	// is the fallback source for the published version's subscribed events.
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
		}, nil
	}
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return []*larkapplication.ApplicationAppVersion{
			{
				VersionId:   strp("published-1"),
				Version:     strp("1.0.0"),
				Status:      intp(larkapplication.AppVersionStatusAudited),
				PublishTime: strp("1700000000"),
				Events:      []string{"im.message.receive_v1"},
				Ability:     &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
			},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_list_fallback"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if len(plan.Diff.MissingEvents) != 0 {
		t.Fatalf("missing events = %#v, want none (event read from published version)", plan.Diff.MissingEvents)
	}
	for _, item := range plan.BlockingRequirements {
		if item.Kind == AutoConfigRequirementKindEvent {
			t.Fatalf("event %q must not be blocking when published version lists it", item.Key)
		}
	}
}

func TestPlanAppAutoConfigEmptyVersionEventsTreatedAsUnverifiable(t *testing.T) {
	restoreAutoConfigHooks(t)
	// A version that reports an empty event list gives no positive evidence of
	// what is subscribed. Per the no-false-positive decision, events do not
	// participate in the missing/blocking lists in that state.
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
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

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_empty_events"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if len(plan.Diff.MissingEvents) != 0 {
		t.Fatalf("missing events = %#v, want none when version events are unverifiable", plan.Diff.MissingEvents)
	}
	for _, item := range plan.BlockingRequirements {
		if item.Kind == AutoConfigRequirementKindEvent {
			t.Fatalf("event %q must not be blocking when version events are unverifiable", item.Key)
		}
	}
}

func TestPlanAppAutoConfigCallbacksUnverifiableWhenCallbackInfoNil(t *testing.T) {
	restoreAutoConfigHooks(t)
	// application.get may omit callback_info for some app states. A nil
	// callback_info gives no positive evidence of what callbacks are
	// subscribed, so callbacks must not be reported missing in that state
	// (same weak-dependency rule as events).
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
			OnlineVersionId: strp("online-1"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp("online-1"),
			Version:   strp("1.0.0"),
			Status:    intp(larkapplication.AppVersionStatusAudited),
			Events:    []string{"im.message.receive_v1"},
			Ability:   &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_no_callback_info"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if len(plan.Diff.MissingCallbacks) != 0 {
		t.Fatalf("missing callbacks = %#v, want none when callback_info is unverifiable", plan.Diff.MissingCallbacks)
	}
	for _, item := range plan.BlockingRequirements {
		if item.Kind == AutoConfigRequirementKindCallback {
			t.Fatalf("callback %q must not be a blocking requirement when callback_info is unverifiable", item.Key)
		}
	}
}

func TestPlanAppAutoConfigCallbackTypeMismatchFlagged(t *testing.T) {
	restoreAutoConfigHooks(t)
	// The callback subscription mode comes from callback_info.callback_type.
	// A non-websocket mode must be reported as a config difference even when
	// the callback list itself is present.
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
			CallbackInfo: &larkapplication.CallbackInfo{
				CallbackType:        strp("webhook"),
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
			Events:    []string{"im.message.receive_v1"},
			Ability:   &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_webhook_callback"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if len(plan.Diff.MissingCallbacks) != 0 {
		t.Fatalf("missing callbacks = %#v, want none when card.action.trigger is subscribed", plan.Diff.MissingCallbacks)
	}
	if !plan.Diff.CallbackTypeMismatch {
		t.Fatalf("callback type mismatch must be flagged for webhook, got %#v", plan.Diff)
	}
	if plan.Diff.CallbackRequestURLMismatch {
		t.Fatalf("callback request url mismatch must not be flagged when no url is configured, got %#v", plan.Diff)
	}
	for _, item := range plan.BlockingRequirements {
		if item.Kind == AutoConfigRequirementKindCallback {
			t.Fatalf("callback %q must not be blocking when it is subscribed", item.Key)
		}
	}
}

func TestPlanAppAutoConfigAlternativeScopeSatisfiesRequirement(t *testing.T) {
	restoreAutoConfigHooks(t)
	// Feishu documents "any one of" permission relationships (for example
	// im:chat / im:chat:read / im:chat:readonly all satisfy chat reads).
	// A configured alternative must satisfy the manifest requirement and must
	// not be reported as an extra scope.
	manifest := feishuapp.Manifest{
		ScopeRequirements: []feishuapp.ScopeRequirement{
			{Scope: "im:chat:readonly", ScopeType: "tenant", Feature: "room_admin", Required: true},
		},
	}
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:chat:read"), TokenTypes: []string{"tenant"}},
			},
			OnlineVersionId: strp("online-1"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp("online-1"),
			Version:   strp("1.0.0"),
			Status:    intp(larkapplication.AppVersionStatusAudited),
			Events:    []string{"im.message.receive_v1"},
			Ability:   &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_alt_scope"},
		manifest,
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	for _, item := range plan.Diff.MissingScopes {
		if item.Scope == "im:chat:readonly" {
			t.Fatalf("im:chat:readonly must not be missing when im:chat:read is configured")
		}
	}
	for _, item := range plan.Diff.ExtraScopes {
		if item.Scope == "im:chat:read" {
			t.Fatalf("im:chat:read must not be extra when it satisfies im:chat:readonly")
		}
	}
	for _, item := range plan.BlockingRequirements {
		if item.Kind == AutoConfigRequirementKindScope && item.Key == "im:chat:readonly" {
			t.Fatalf("im:chat:readonly must not be a blocking requirement when im:chat:read is configured")
		}
	}
}

func TestPlanAppAutoConfigAlternativeScopeStillMissingWithoutAnySatisfier(t *testing.T) {
	restoreAutoConfigHooks(t)
	manifest := feishuapp.Manifest{
		ScopeRequirements: []feishuapp.ScopeRequirement{
			{Scope: "im:chat:readonly", ScopeType: "tenant", Feature: "room_admin", Required: true},
		},
	}
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
			},
			OnlineVersionId: strp("online-1"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(context.Context, *FeishuCallBroker, *lark.Client, string, string) (*larkapplication.ApplicationAppVersion, error) {
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp("online-1"),
			Version:   strp("1.0.0"),
			Status:    intp(larkapplication.AppVersionStatusAudited),
			Events:    []string{"im.message.receive_v1"},
			Ability:   &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_no_alt"},
		manifest,
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	found := false
	for _, item := range plan.Diff.MissingScopes {
		if item.Scope == "im:chat:readonly" {
			found = true
		}
	}
	if !found {
		t.Fatalf("im:chat:readonly must be missing when neither it nor an alternative is configured, got %#v", plan.Diff.MissingScopes)
	}
}

func TestNarrowedManifestScopeSatisfiersHonorAlternatives(t *testing.T) {
	// After the manifest narrows to the minimum requested scope, configured
	// broader/legacy scopes that Feishu documents as "any one of" alternatives
	// must still satisfy the requirement so existing installs are not flagged
	// as missing.
	cases := []struct {
		requirement string
		configured  string
	}{
		{requirement: "im:message:readonly", configured: "im:message"},
		{requirement: "im:resource:upload", configured: "im:resource"},
		{requirement: "application:application:self_manage", configured: "admin:app.info:readonly"},
		{requirement: "im:chat:readonly", configured: "im:chat"},
		{requirement: "im:message.group_at_msg.include_bot:readonly", configured: "im:message.group_at_msg.include_bot"},
		{requirement: "im:message.group_msg:readonly", configured: "im:message.group_msg"},
	}
	for _, tc := range cases {
		req := AutoConfigScopeRef{Scope: tc.requirement, ScopeType: "tenant"}
		configured := map[string]bool{scopeKey(tc.configured, "tenant"): true}
		if !scopeRequirementSatisfied(req, configured) {
			t.Fatalf("%q must be satisfied by configured %q", tc.requirement, tc.configured)
		}
		if got := missingScopeRefs([]AutoConfigScopeRef{req}, []AutoConfigScopeRef{{Scope: tc.configured, ScopeType: "tenant"}}); len(got) != 0 {
			t.Fatalf("%q reported missing with configured %q: %#v", tc.requirement, tc.configured, got)
		}
	}

	// A configured alternative must not be reported as an extra scope.
	extra := extraScopeRefs(
		[]AutoConfigScopeRef{{Scope: "im:message", ScopeType: "tenant"}},
		[]AutoConfigScopeRef{{Scope: "im:message:readonly", ScopeType: "tenant"}},
	)
	if len(extra) != 0 {
		t.Fatalf("configured im:message must not be extra when im:message:readonly is required, got %#v", extra)
	}
}

func TestNarrowedManifestScopeSatisfierExcludesPartialLegacyScope(t *testing.T) {
	// im:message.history:readonly covers message.get per official docs but does
	// not cover the recalled event; the manifest requirement im:message:readonly
	// backs both, so the legacy history scope must not be treated as satisfied.
	req := AutoConfigScopeRef{Scope: "im:message:readonly", ScopeType: "tenant"}
	configured := map[string]bool{scopeKey("im:message.history:readonly", "tenant"): true}
	if scopeRequirementSatisfied(req, configured) {
		t.Fatal("im:message.history:readonly must not satisfy im:message:readonly: it does not cover the recalled event")
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
			CallbackInfo: &larkapplication.CallbackInfo{
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
	oldListVersions := autoConfigListApplicationVersions
	t.Cleanup(func() {
		autoConfigGetApplication = oldGetApp
		autoConfigGetApplicationVersion = oldGetVersion
		autoConfigListApplicationVersions = oldListVersions
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
