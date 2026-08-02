package feishu

import (
	"context"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkapplicationv7 "github.com/larksuite/oapi-sdk-go/v3/service/application/v7"
)

type v7PatchConfigRequest struct {
	Scope    *v7PatchConfigScope    `json:"scope,omitempty"`
	Event    *v7PatchConfigEvent    `json:"event,omitempty"`
	Callback *v7PatchConfigCallback `json:"callback,omitempty"`
}

type v7PatchConfigScope struct {
	AddScopes    []v7PatchConfigScopeItem `json:"add_scopes,omitempty"`
	RemoveScopes []v7PatchConfigScopeItem `json:"remove_scopes,omitempty"`
}

type v7PatchConfigScopeItem struct {
	ScopeName string `json:"scope_name"`
	TokenType string `json:"token_type"`
}

type v7PatchConfigEvent struct {
	SubscriptionType string   `json:"subscription_type"`
	RequestURL       *string  `json:"request_url,omitempty"`
	AddEvents        []string `json:"add_events,omitempty"`
	RemoveEvents     []string `json:"remove_events,omitempty"`
}

type v7PatchConfigCallback struct {
	CallbackType    string   `json:"callback_type"`
	RequestURL      *string  `json:"request_url,omitempty"`
	AddCallbacks    []string `json:"add_callbacks,omitempty"`
	RemoveCallbacks []string `json:"remove_callbacks,omitempty"`
}

type v7PatchAbilityRequest struct {
	Bot *v7PatchAbilityBot `json:"bot,omitempty"`
}

type v7PatchAbilityBot struct {
	Enable bool `json:"enable"`
}

type v7PublishRequest struct {
	MobileDefaultAbility string `json:"mobile_default_ability,omitempty"`
	PcDefaultAbility     string `json:"pc_default_ability,omitempty"`
	Remark               string `json:"remark"`
	Changelog            string `json:"changelog"`
	Version              string `json:"version,omitempty"`
}

func patchV7AppConfig(ctx context.Context, broker *FeishuCallBroker, client *lark.Client, appID string, req v7PatchConfigRequest) error {
	resp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "application.v7.application.config.patch",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkapplicationv7.PatchApplicationConfigResp, error) {
		return sdkClient.Application.V7.ApplicationConfig.Patch(callCtx, toSDKPatchApplicationConfigReq(appID, req))
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("application.v7.application.config.patch returned nil response")
	}
	if !resp.Success() {
		return newAPIError("application.v7.application.config.patch", resp.ApiResp, resp.CodeError)
	}
	return nil
}

func patchV7AppAbility(ctx context.Context, broker *FeishuCallBroker, client *lark.Client, appID string, req v7PatchAbilityRequest) error {
	resp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "application.v7.application.ability.patch",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkapplicationv7.PatchApplicationAbilityResp, error) {
		return sdkClient.Application.V7.ApplicationAbility.Patch(callCtx, toSDKPatchApplicationAbilityReq(appID, req))
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("application.v7.application.ability.patch returned nil response")
	}
	if !resp.Success() {
		return newAPIError("application.v7.application.ability.patch", resp.ApiResp, resp.CodeError)
	}
	return nil
}

func publishV7App(ctx context.Context, broker *FeishuCallBroker, client *lark.Client, appID string, req v7PublishRequest) (string, string, error) {
	resp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "application.v7.application.publish.create",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkapplicationv7.CreateApplicationPublishResp, error) {
		return sdkClient.Application.V7.ApplicationPublish.Create(callCtx, toSDKCreateApplicationPublishReq(appID, req))
	})
	if err != nil {
		return "", "", err
	}
	if resp == nil {
		return "", "", fmt.Errorf("application.v7.application.publish.create returned nil response")
	}
	if !resp.Success() {
		return "", "", newAPIError("application.v7.application.publish.create", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return "", "", nil
	}
	return strings.TrimSpace(stringValue(resp.Data.VersionId)), strings.TrimSpace(stringValue(resp.Data.Version)), nil
}

func toSDKPatchApplicationConfigReq(appID string, req v7PatchConfigRequest) *larkapplicationv7.PatchApplicationConfigReq {
	return larkapplicationv7.NewPatchApplicationConfigReqBuilder().
		AppId(strings.TrimSpace(appID)).
		Body(toSDKPatchApplicationConfigReqBody(req)).
		Build()
}

func toSDKPatchApplicationConfigReqBody(req v7PatchConfigRequest) *larkapplicationv7.PatchApplicationConfigReqBody {
	return &larkapplicationv7.PatchApplicationConfigReqBody{
		Scope:    toSDKAppConfigScope(req.Scope),
		Event:    toSDKAppConfigEvent(req.Event),
		Callback: toSDKAppConfigCallback(req.Callback),
	}
}

func toSDKAppConfigScope(scope *v7PatchConfigScope) *larkapplicationv7.AppConfigScope {
	if scope == nil {
		return nil
	}
	return &larkapplicationv7.AppConfigScope{
		AddScopes:    toSDKAppConfigScopeItems(scope.AddScopes),
		RemoveScopes: toSDKAppConfigScopeItems(scope.RemoveScopes),
	}
}

func toSDKAppConfigScopeItems(items []v7PatchConfigScopeItem) []*larkapplicationv7.AppConfigScopeItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]*larkapplicationv7.AppConfigScopeItem, 0, len(items))
	for _, item := range items {
		out = append(out, &larkapplicationv7.AppConfigScopeItem{
			ScopeName: v7StringPtr(strings.TrimSpace(item.ScopeName)),
			TokenType: v7StringPtr(strings.TrimSpace(item.TokenType)),
		})
	}
	return out
}

func toSDKAppConfigEvent(event *v7PatchConfigEvent) *larkapplicationv7.AppConfigEvent {
	if event == nil {
		return nil
	}
	return &larkapplicationv7.AppConfigEvent{
		SubscriptionType: v7StringPtr(strings.TrimSpace(event.SubscriptionType)),
		RequestUrl:       cloneStringPtr(event.RequestURL),
		AddEvents:        append([]string(nil), event.AddEvents...),
		RemoveEvents:     append([]string(nil), event.RemoveEvents...),
	}
}

func toSDKAppConfigCallback(callback *v7PatchConfigCallback) *larkapplicationv7.AppConfigCallback {
	if callback == nil {
		return nil
	}
	return &larkapplicationv7.AppConfigCallback{
		CallbackType:    v7StringPtr(strings.TrimSpace(callback.CallbackType)),
		RequestUrl:      cloneStringPtr(callback.RequestURL),
		AddCallbacks:    append([]string(nil), callback.AddCallbacks...),
		RemoveCallbacks: append([]string(nil), callback.RemoveCallbacks...),
	}
}

func toSDKPatchApplicationAbilityReq(appID string, req v7PatchAbilityRequest) *larkapplicationv7.PatchApplicationAbilityReq {
	return larkapplicationv7.NewPatchApplicationAbilityReqBuilder().
		AppId(strings.TrimSpace(appID)).
		Body(toSDKPatchApplicationAbilityReqBody(req)).
		Build()
}

func toSDKPatchApplicationAbilityReqBody(req v7PatchAbilityRequest) *larkapplicationv7.PatchApplicationAbilityReqBody {
	var bot *larkapplicationv7.AppAbilityBot
	if req.Bot != nil {
		bot = &larkapplicationv7.AppAbilityBot{Enable: v7BoolPtr(req.Bot.Enable)}
	}
	return &larkapplicationv7.PatchApplicationAbilityReqBody{Bot: bot}
}

func toSDKCreateApplicationPublishReq(appID string, req v7PublishRequest) *larkapplicationv7.CreateApplicationPublishReq {
	return larkapplicationv7.NewCreateApplicationPublishReqBuilder().
		AppId(strings.TrimSpace(appID)).
		Body(toSDKCreateApplicationPublishReqBody(req)).
		Build()
}

func toSDKCreateApplicationPublishReqBody(req v7PublishRequest) *larkapplicationv7.CreateApplicationPublishReqBody {
	body := &larkapplicationv7.CreateApplicationPublishReqBody{
		Remark:    v7StringPtr(req.Remark),
		Changelog: v7StringPtr(req.Changelog),
	}
	if value := strings.TrimSpace(req.MobileDefaultAbility); value != "" {
		body.MobileDefaultAbility = v7StringPtr(value)
	}
	if value := strings.TrimSpace(req.PcDefaultAbility); value != "" {
		body.PcDefaultAbility = v7StringPtr(value)
	}
	if value := strings.TrimSpace(req.Version); value != "" {
		body.Version = v7StringPtr(value)
	}
	return body
}

func v7StringPtr(value string) *string {
	return &value
}

func v7BoolPtr(value bool) *bool {
	return &value
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
