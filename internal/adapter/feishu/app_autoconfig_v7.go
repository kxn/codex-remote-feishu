package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
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

type v7CodeResponse struct {
	larkcore.CodeError
}

type v7PublishResponse struct {
	larkcore.CodeError
	Data *struct {
		VersionID string `json:"version_id,omitempty"`
		Version   string `json:"version,omitempty"`
	} `json:"data,omitempty"`
}

func patchV7AppConfig(ctx context.Context, broker *FeishuCallBroker, client *lark.Client, appID string, req v7PatchConfigRequest) error {
	apiResp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "application.v7.application.config.patch",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkcore.ApiResp, error) {
		return sdkClient.Patch(callCtx, v7AppPath(appID, "config"), req, larkcore.AccessTokenTypeTenant)
	})
	if err != nil {
		return err
	}
	var decoded v7CodeResponse
	if err := json.Unmarshal(apiResp.RawBody, &decoded); err != nil {
		return err
	}
	if decoded.Code != 0 {
		return newAPIError("application.v7.application.config.patch", apiResp, decoded.CodeError)
	}
	return nil
}

func patchV7AppAbility(ctx context.Context, broker *FeishuCallBroker, client *lark.Client, appID string, req v7PatchAbilityRequest) error {
	apiResp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "application.v7.application.ability.patch",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkcore.ApiResp, error) {
		return sdkClient.Patch(callCtx, v7AppPath(appID, "ability"), req, larkcore.AccessTokenTypeTenant)
	})
	if err != nil {
		return err
	}
	var decoded v7CodeResponse
	if err := json.Unmarshal(apiResp.RawBody, &decoded); err != nil {
		return err
	}
	if decoded.Code != 0 {
		return newAPIError("application.v7.application.ability.patch", apiResp, decoded.CodeError)
	}
	return nil
}

func publishV7App(ctx context.Context, broker *FeishuCallBroker, client *lark.Client, appID string, req v7PublishRequest) (string, string, error) {
	apiResp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "application.v7.application.publish.create",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkcore.ApiResp, error) {
		return sdkClient.Post(callCtx, v7AppPath(appID, "publish"), req, larkcore.AccessTokenTypeTenant)
	})
	if err != nil {
		return "", "", err
	}
	var decoded v7PublishResponse
	if err := json.Unmarshal(apiResp.RawBody, &decoded); err != nil {
		return "", "", err
	}
	if decoded.Code != 0 {
		return "", "", newAPIError("application.v7.application.publish.create", apiResp, decoded.CodeError)
	}
	if decoded.Data == nil {
		return "", "", nil
	}
	return strings.TrimSpace(decoded.Data.VersionID), strings.TrimSpace(decoded.Data.Version), nil
}

func v7AppPath(appID string, tail string) string {
	return fmt.Sprintf("/open-apis/application/v7/applications/%s/%s", url.PathEscape(strings.TrimSpace(appID)), strings.TrimSpace(tail))
}
