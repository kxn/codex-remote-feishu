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
		}, nil
	}
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return []*larkapplication.ApplicationAppVersion{
			{
				VersionId:   strp("published-1"),
				Version:     strp("1.0.0"),
				Status:      intp(larkapplication.AppVersionStatusAudited),
				PublishTime: strp("1700000000"),
				EventInfos:  []*larkapplication.Event{{EventType: strp("some.other.event_v1")}},
			},
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

func TestPlanAppAutoConfigDefaultManifestReportsPrimaryBootstrapRequirements(t *testing.T) {
	restoreAutoConfigHooks(t)
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("application:application:self_manage"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
				{Scope: strp("bitable:app"), TokenTypes: []string{"tenant"}},
				{Scope: strp("im:message:readonly"), TokenTypes: []string{"tenant"}},
				{Scope: strp("im:message.group_at_msg:readonly"), TokenTypes: []string{"tenant"}},
				{Scope: strp("im:message.group_at_msg.include_bot:readonly"), TokenTypes: []string{"tenant"}},
				{Scope: strp("im:message.group_msg"), TokenTypes: []string{"tenant"}},
				{Scope: strp("im:message.p2p_msg:readonly"), TokenTypes: []string{"tenant"}},
				{Scope: strp("im:message.reactions:read"), TokenTypes: []string{"tenant"}},
				{Scope: strp("im:message.reactions:write_only"), TokenTypes: []string{"tenant"}},
				{Scope: strp("im:message:send_as_bot"), TokenTypes: []string{"tenant"}},
				{Scope: strp("im:resource:upload"), TokenTypes: []string{"tenant"}},
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
			Ability:   &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
		}, nil
	}
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return []*larkapplication.ApplicationAppVersion{
			{
				VersionId:   strp("published-1"),
				Version:     strp("1.0.0"),
				Status:      intp(larkapplication.AppVersionStatusAudited),
				PublishTime: strp("1700000000"),
				EventInfos: []*larkapplication.Event{
					{EventType: strp("im.message.receive_v1")},
					{EventType: strp("im.message.recalled_v1")},
					{EventType: strp("im.message.reaction.created_v1")},
					{EventType: strp("im.message.reaction.deleted_v1")},
					{EventType: strp("application.bot.menu_v6")},
				},
			},
		}, nil
	}

	plan, err := PlanAppAutoConfig(context.Background(), LiveGatewayConfig{GatewayID: "main", AppID: "cli_xxx"}, feishuapp.DefaultManifest(), feishuapp.DefaultFixedPolicy())
	if err != nil {
		t.Fatalf("PlanAppAutoConfig: %v", err)
	}
	if !containsScopeRef(plan.Diff.MissingScopes, "im:chat:readonly", "tenant") {
		t.Fatalf("missing scopes = %#v, want im:chat:readonly tenant", plan.Diff.MissingScopes)
	}
	if !reflect.DeepEqual(plan.Diff.MissingEvents, []string{"im.chat.member.bot.added_v1"}) {
		t.Fatalf("missing events = %#v, want bot added event", plan.Diff.MissingEvents)
	}
	if !hasRequirement(plan.BlockingRequirements, AutoConfigRequirementKindScope, "im:chat:readonly") {
		t.Fatalf("blocking requirements = %#v, want im:chat:readonly", plan.BlockingRequirements)
	}
	if !hasRequirement(plan.BlockingRequirements, AutoConfigRequirementKindEvent, "im.chat.member.bot.added_v1") {
		t.Fatalf("blocking requirements = %#v, want bot added event", plan.BlockingRequirements)
	}
}

func TestPlanAppAutoConfigUsesPublishedEventInfosInsteadOfLocalizedEvents(t *testing.T) {
	restoreAutoConfigHooks(t)
	// Feishu's app_versions response can expose localized display names in
	// events while event_infos.event_type carries the stable event key. The
	// published version is the CLI's source of truth even when application.get
	// also reports an unaudited version.
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
			CallbackInfo: &larkapplication.CallbackInfo{
				CallbackType:        strp("websocket"),
				SubscribedCallbacks: []string{"card.action.trigger"},
			},
			OnlineVersionId:  strp("online-1"),
			UnauditVersionId: strp("draft-1"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(_ context.Context, _ *FeishuCallBroker, _ *lark.Client, _ string, versionID string) (*larkapplication.ApplicationAppVersion, error) {
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp(versionID),
			Version:   strp("1.0.1"),
			Status:    intp(larkapplication.AppVersionStatusUnaudit),
			Events:    []string{"草稿事件名称"},
			Ability:   &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
		}, nil
	}
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return []*larkapplication.ApplicationAppVersion{
			{
				VersionId:   strp("published-1"),
				Version:     strp("1.0.0"),
				Status:      intp(larkapplication.AppVersionStatusAudited),
				PublishTime: strp("1785802424"),
				Events:      []string{"接收消息"},
				EventInfos:  []*larkapplication.Event{{EventType: strp("im.message.receive_v1")}},
				Ability:     &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
			},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_published_event_infos"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if len(plan.Diff.MissingEvents) != 0 {
		t.Fatalf("missing events = %#v, want none from published event_infos", plan.Diff.MissingEvents)
	}
	if !reflect.DeepEqual(plan.Current.ConfiguredEvents, []string{"im.message.receive_v1"}) {
		t.Fatalf("configured events = %#v, want published event keys", plan.Current.ConfiguredEvents)
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
				Events:      []string{"接收消息"},
				EventInfos: []*larkapplication.Event{
					{EventType: strp("im.message.receive_v1")},
				},
				Ability: &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
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

func TestPlanAppAutoConfigEventsIgnoreUnauditedVersion(t *testing.T) {
	restoreAutoConfigHooks(t)
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
			OnlineVersionId:  strp("online-1"),
			UnauditVersionId: strp("draft-2"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(_ context.Context, _ *FeishuCallBroker, _ *lark.Client, _ string, versionID string) (*larkapplication.ApplicationAppVersion, error) {
		if versionID == "draft-2" {
			return &larkapplication.ApplicationAppVersion{
				VersionId: strp(versionID),
				Status:    intp(larkapplication.AppVersionStatusUnaudit),
			}, nil
		}
		return &larkapplication.ApplicationAppVersion{
			VersionId:   strp(versionID),
			Status:      intp(larkapplication.AppVersionStatusAudited),
			PublishTime: strp("1700000000"),
			EventInfos: []*larkapplication.Event{
				{EventType: strp("im.message.receive_v1")},
			},
		}, nil
	}
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return []*larkapplication.ApplicationAppVersion{
			{
				VersionId:   strp("published-1"),
				Status:      intp(larkapplication.AppVersionStatusAudited),
				PublishTime: strp("1700000000"),
				EventInfos: []*larkapplication.Event{
					{EventType: strp("im.message.receive_v1")},
				},
			},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_unaudit_events"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if len(plan.Diff.MissingEvents) != 0 {
		t.Fatalf("missing events = %#v, want none from the published version", plan.Diff.MissingEvents)
	}
	if !reflect.DeepEqual(plan.Current.ConfiguredEvents, []string{"im.message.receive_v1"}) {
		t.Fatalf("configured events = %#v, want the published version events", plan.Current.ConfiguredEvents)
	}
	for _, item := range plan.BlockingRequirements {
		if item.Kind == AutoConfigRequirementKindEvent {
			t.Fatalf("event %q must not be blocking when the published version contains it", item.Key)
		}
	}
}

func TestPlanAppAutoConfigEventsPreferPublishedVersionList(t *testing.T) {
	restoreAutoConfigHooks(t)
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
			},
			OnlineVersionId:  strp("online-1"),
			UnauditVersionId: strp("draft-2"),
		}, nil
	}
	autoConfigGetApplicationVersion = func(_ context.Context, _ *FeishuCallBroker, _ *lark.Client, _ string, versionID string) (*larkapplication.ApplicationAppVersion, error) {
		return &larkapplication.ApplicationAppVersion{
			VersionId: strp(versionID),
			Status:    intp(larkapplication.AppVersionStatusAudited),
			Events:    []string{"some.stale.event_v1"},
		}, nil
	}
	listCalls := 0
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		listCalls++
		return []*larkapplication.ApplicationAppVersion{
			{
				VersionId:   strp("published-1"),
				Status:      intp(larkapplication.AppVersionStatusAudited),
				PublishTime: strp("1700000000"),
				EventInfos: []*larkapplication.Event{
					{EventType: strp("im.message.receive_v1")},
				},
			},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_prefer_published"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if listCalls != 1 {
		t.Fatalf("published version list calls = %d, want 1", listCalls)
	}
	if len(plan.Diff.MissingEvents) != 0 {
		t.Fatalf("missing events = %#v, want events from the published version list", plan.Diff.MissingEvents)
	}
	if !reflect.DeepEqual(plan.Current.ConfiguredEvents, []string{"im.message.receive_v1"}) {
		t.Fatalf("configured events = %#v, want the published version list events", plan.Current.ConfiguredEvents)
	}
}

func TestPlanAppAutoConfigPublishedVersionWithEmptyEventsReportsMissing(t *testing.T) {
	restoreAutoConfigHooks(t)
	// Match lark-cli: once a published version is readable, an empty
	// event_infos list is definitive evidence that no console event is enabled.
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
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return []*larkapplication.ApplicationAppVersion{
			{
				VersionId:   strp("published-1"),
				Version:     strp("1.0.0"),
				Status:      intp(larkapplication.AppVersionStatusAudited),
				PublishTime: strp("1700000000"),
				EventInfos:  []*larkapplication.Event{},
			},
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
	if !reflect.DeepEqual(plan.Diff.MissingEvents, []string{"im.message.receive_v1"}) {
		t.Fatalf("missing events = %#v, want required event when published event_infos is empty", plan.Diff.MissingEvents)
	}
	foundEvent := false
	for _, item := range plan.BlockingRequirements {
		if item.Kind == AutoConfigRequirementKindEvent {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Fatalf("published version with empty event_infos must make the event blocking")
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

func TestNarrowedManifestScopeSatisfiersHonorAlternatives(t *testing.T) {
	// Configured broader/legacy scopes that Feishu documents as "any one of"
	// alternatives must still satisfy the current requirement so existing
	// installs are not flagged as missing.
	cases := []struct {
		requirement string
		configured  string
	}{
		{requirement: "im:message:readonly", configured: "im:message"},
		{requirement: "im:resource:upload", configured: "im:resource"},
		{requirement: "application:application:self_manage", configured: "admin:app.info:readonly"},
		{requirement: "im:message.group_at_msg.include_bot:readonly", configured: "im:message.group_at_msg.include_bot"},
		{requirement: "im:message.group_msg", configured: "im:message.group_msg:readonly"},
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

	// A configured historical alternative must not be reported as an extra scope.
	extra := extraScopeRefs(
		[]AutoConfigScopeRef{{Scope: "im:message.group_msg:readonly", ScopeType: "tenant"}},
		[]AutoConfigScopeRef{{Scope: "im:message.group_msg", ScopeType: "tenant"}},
	)
	if len(extra) != 0 {
		t.Fatalf("configured im:message.group_msg:readonly must not be extra when im:message.group_msg is required, got %#v", extra)
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
			Events:    []string{"接收消息"},
		}, nil
	}
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return []*larkapplication.ApplicationAppVersion{
			{
				VersionId:   strp("published-1"),
				Version:     strp("1.0.0"),
				Status:      intp(larkapplication.AppVersionStatusAudited),
				PublishTime: strp("1700000000"),
				Events:      []string{"接收消息"},
				EventInfos:  []*larkapplication.Event{{EventType: strp("im.message.receive_v1")}},
				Ability:     &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
			},
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

func TestPlanAppAutoConfigIgnoresExtraConfiguredItemsWhenRequirementsAreSatisfied(t *testing.T) {
	restoreAutoConfigHooks(t)
	autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
		return &larkapplication.Application{
			Scopes: []*larkapplication.AppScope{
				{Scope: strp("im:message"), TokenTypes: []string{"tenant"}},
				{Scope: strp("drive:drive"), TokenTypes: []string{"tenant"}},
				{Scope: strp("calendar:calendar"), TokenTypes: []string{"tenant"}},
			},
			CallbackInfo: &larkapplication.CallbackInfo{
				CallbackType:        strp("websocket"),
				SubscribedCallbacks: []string{"card.action.trigger", "card.action.extra"},
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
		}, nil
	}
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return []*larkapplication.ApplicationAppVersion{
			{
				VersionId:   strp("published-1"),
				Version:     strp("1.0.0"),
				Status:      intp(larkapplication.AppVersionStatusAudited),
				PublishTime: strp("1700000000"),
				EventInfos: []*larkapplication.Event{
					{EventType: strp("im.message.receive_v1")},
					{EventType: strp("im.message.extra_v1")},
				},
				Ability: &larkapplication.AppAbility{Bot: &larkapplication.Bot{}},
			},
		}, nil
	}

	plan, err := PlanAppAutoConfig(
		context.Background(),
		LiveGatewayConfig{GatewayID: "main", AppID: "cli_extra_configured"},
		testAutoConfigManifest(),
		feishuapp.DefaultFixedPolicy(),
	)
	if err != nil {
		t.Fatalf("PlanAppAutoConfig returned error: %v", err)
	}
	if plan.Status != AutoConfigStatusClean {
		t.Fatalf("plan status = %q, want %q (diff=%#v)", plan.Status, AutoConfigStatusClean, plan.Diff)
	}
	if plan.Diff.ConfigPatchRequired {
		t.Fatalf("extra configured items must not require a config patch: %#v", plan.Diff)
	}
	if !reflect.DeepEqual(plan.Diff.ExtraScopes, []AutoConfigScopeRef{{Scope: "calendar:calendar", ScopeType: "tenant"}}) {
		t.Fatalf("extra scopes = %#v, want diagnostic-only extra scope", plan.Diff.ExtraScopes)
	}
	if !reflect.DeepEqual(plan.Diff.ExtraEvents, []string{"im.message.extra_v1"}) {
		t.Fatalf("extra events = %#v, want diagnostic-only extra event", plan.Diff.ExtraEvents)
	}
	if !reflect.DeepEqual(plan.Diff.ExtraCallbacks, []string{"card.action.extra"}) {
		t.Fatalf("extra callbacks = %#v, want diagnostic-only extra callback", plan.Diff.ExtraCallbacks)
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

func TestPlanAppAutoConfigReadPermissionErrorsReturnActionableMissingScope(t *testing.T) {
	tests := []struct {
		name string
		err  *APIError
	}{
		{
			name: "forbidden",
			err: &APIError{
				API:        "application.v6.application.get",
				Code:       99991663,
				Msg:        "permission denied",
				StatusCode: http.StatusForbidden,
			},
		},
		{
			name: "permission violations",
			err: &APIError{
				API:        "application.v6.application.get",
				Code:       99991663,
				Msg:        "permission denied",
				StatusCode: http.StatusBadRequest,
				PermissionViolations: []APIErrorPermissionViolation{
					{Type: "tenant", Subject: "application:application:self_manage"},
				},
			},
		},
		{
			name: "extracted permission gap",
			err: &APIError{
				API:        "application.v6.application.get",
				Code:       99991663,
				Msg:        "missing application:application:self_manage",
				StatusCode: http.StatusBadRequest,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreAutoConfigHooks(t)
			autoConfigGetApplication = func(context.Context, *FeishuCallBroker, *lark.Client, string) (*larkapplication.Application, error) {
				return nil, tt.err
			}

			plan, err := PlanAppAutoConfig(
				context.Background(),
				LiveGatewayConfig{GatewayID: "main", AppID: "cli_permission"},
				feishuapp.DefaultManifest(),
				feishuapp.DefaultFixedPolicy(),
			)
			if err != nil {
				t.Fatalf("PlanAppAutoConfig returned error: %v", err)
			}
			assertAppManagementPermissionPlan(t, plan)
		})
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
	autoConfigListApplicationVersions = func(context.Context, *FeishuCallBroker, *lark.Client, string) ([]*larkapplication.ApplicationAppVersion, error) {
		return nil, nil
	}
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

func containsScopeRef(values []AutoConfigScopeRef, scope, scopeType string) bool {
	for _, item := range values {
		if item.Scope == scope && item.ScopeType == scopeType {
			return true
		}
	}
	return false
}

func hasRequirement(values []AutoConfigRequirementStatus, kind string, key string) bool {
	for _, item := range values {
		if item.Kind == kind && item.Key == key {
			return true
		}
	}
	return false
}

func assertAppManagementPermissionPlan(t *testing.T, plan AutoConfigPlan) {
	t.Helper()
	if plan.Status != AutoConfigStatusApplyRequired {
		t.Fatalf("plan status = %q, want %q", plan.Status, AutoConfigStatusApplyRequired)
	}
	if plan.BlockingReason != "" {
		t.Fatalf("blocking reason = %q, want empty actionable plan", plan.BlockingReason)
	}
	if !plan.Diff.ConfigPatchRequired {
		t.Fatalf("config patch must be required for missing scope: %#v", plan.Diff)
	}
	if !reflect.DeepEqual(plan.Diff.MissingScopes, []AutoConfigScopeRef{{
		Scope:     "application:application:self_manage",
		ScopeType: "tenant",
	}}) {
		t.Fatalf("missing scopes = %#v, want only application self-manage tenant", plan.Diff.MissingScopes)
	}
	if len(plan.BlockingRequirements) != 1 {
		t.Fatalf("blocking requirements = %#v, want one known scope", plan.BlockingRequirements)
	}
	req := plan.BlockingRequirements[0]
	if req.Kind != AutoConfigRequirementKindScope ||
		req.Key != "application:application:self_manage" ||
		req.ScopeType != "tenant" ||
		!req.Required ||
		req.Present {
		t.Fatalf("unexpected blocking requirement: %#v", req)
	}
	if len(plan.Diff.MissingEvents) != 0 || len(plan.Diff.MissingCallbacks) != 0 {
		t.Fatalf("read permission fallback must not infer events/callbacks: %#v", plan.Diff)
	}
}

func strp(value string) *string {
	return &value
}

func intp(value int) *int {
	return &value
}
