package feishu

import (
	"context"
	"reflect"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"

	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

func TestPlanAppAutoConfigReportsDiffAndRequirementState(t *testing.T) {
	restoreAutoConfigHooks(t)
	autoConfigListScopes = func(context.Context, LiveGatewayConfig) ([]AppScopeStatus, error) {
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
	if len(plan.BlockingRequirements) != 2 {
		t.Fatalf("blocking requirements = %#v", plan.BlockingRequirements)
	}
	if len(plan.DegradableRequirements) != 1 || plan.DegradableRequirements[0].Key != "drive:drive" {
		t.Fatalf("degradable requirements = %#v", plan.DegradableRequirements)
	}
}

func TestApplyAppAutoConfigEnablesBotBeforeConfigPatch(t *testing.T) {
	restoreAutoConfigHooks(t)
	phase := 0
	var calls []string
	autoConfigListScopes = func(context.Context, LiveGatewayConfig) ([]AppScopeStatus, error) {
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
	autoConfigListScopes = func(context.Context, LiveGatewayConfig) ([]AppScopeStatus, error) {
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
			{Scope: "drive:drive", ScopeType: "tenant", Feature: "preview", Required: false, DegradeMessage: "markdown preview disabled"},
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
