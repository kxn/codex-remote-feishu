package daemon

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

type feishuManifestResponse struct {
	Manifest feishuapp.Manifest `json:"manifest"`
}

type feishuAppsResponse struct {
	Apps []adminFeishuAppSummary `json:"apps"`
}

type feishuAppResponse struct {
	App        adminFeishuAppSummary        `json:"app"`
	Mutation   *feishuAppMutationView       `json:"mutation,omitempty"`
	AutoConfig *feishuAppAutoConfigPlanView `json:"autoConfig,omitempty"`
}

type feishuAppVerifyResponse struct {
	App    adminFeishuAppSummary `json:"app"`
	Result feishu.VerifyResult   `json:"result"`
}

type feishuAppAutoConfigPlanResponse struct {
	App  adminFeishuAppSummary `json:"app"`
	Plan feishu.AutoConfigPlan `json:"plan"`
}

type feishuAppAutoConfigPlanView struct {
	Plan  feishu.AutoConfigPlan `json:"plan"`
	Error string                `json:"error,omitempty"`
}

type feishuRuntimeApplyErrorDetails struct {
	GatewayID string                 `json:"gatewayId,omitempty"`
	App       *adminFeishuAppSummary `json:"app,omitempty"`
}

type feishuAppWriteRequest struct {
	ID        string  `json:"id,omitempty"`
	Name      *string `json:"name,omitempty"`
	AppID     *string `json:"appId,omitempty"`
	AppSecret *string `json:"appSecret,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
}

type adminFeishuRuntimeApplyView struct {
	Pending        bool       `json:"pending"`
	Action         string     `json:"action,omitempty"`
	Error          string     `json:"error,omitempty"`
	UpdatedAt      *time.Time `json:"updatedAt,omitempty"`
	RetryAvailable bool       `json:"retryAvailable,omitempty"`
}

type adminFeishuAppSummary struct {
	ID              string                       `json:"id"`
	Name            string                       `json:"name,omitempty"`
	AppID           string                       `json:"appId,omitempty"`
	ConsoleLinks    feishuAppConsoleLinks        `json:"consoleLinks,omitempty"`
	HasSecret       bool                         `json:"hasSecret"`
	Enabled         bool                         `json:"enabled"`
	VerifiedAt      *time.Time                   `json:"verifiedAt,omitempty"`
	Persisted       bool                         `json:"persisted"`
	RuntimeOnly     bool                         `json:"runtimeOnly,omitempty"`
	RuntimeOverride bool                         `json:"runtimeOverride,omitempty"`
	ReadOnly        bool                         `json:"readOnly,omitempty"`
	ReadOnlyReason  string                       `json:"readOnlyReason,omitempty"`
	Status          *feishu.GatewayStatus        `json:"status,omitempty"`
	RuntimeApply    *adminFeishuRuntimeApplyView `json:"runtimeApply,omitempty"`
}

type feishuAppConsoleLinks struct {
	Auth     string `json:"auth,omitempty"`
	Events   string `json:"events,omitempty"`
	Callback string `json:"callback,omitempty"`
	Bot      string `json:"bot,omitempty"`
}

type feishuAppMutationView struct {
	Kind               string `json:"kind,omitempty"`
	Message            string `json:"message,omitempty"`
	ReconnectRequested bool   `json:"reconnectRequested,omitempty"`
	RequiresNewChat    bool   `json:"requiresNewChat,omitempty"`
}
