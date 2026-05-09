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
	App      adminFeishuAppSummary  `json:"app"`
	Mutation *feishuAppMutationView `json:"mutation,omitempty"`
}

type feishuAppVerifyResponse struct {
	App    adminFeishuAppSummary `json:"app"`
	Result feishu.VerifyResult   `json:"result"`
}

type feishuAppPermissionCheckResponse struct {
	App           adminFeishuAppSummary          `json:"app"`
	Ready         bool                           `json:"ready"`
	MissingScopes []feishuAppPermissionCheckItem `json:"missingScopes,omitempty"`
	GrantJSON     string                         `json:"grantJSON,omitempty"`
	LastCheckedAt *time.Time                     `json:"lastCheckedAt,omitempty"`
}

type feishuAppAutoConfigPlanResponse struct {
	App  adminFeishuAppSummary `json:"app"`
	Plan feishu.AutoConfigPlan `json:"plan"`
}

type feishuAppAutoConfigApplyResponse struct {
	App    adminFeishuAppSummary        `json:"app"`
	Result feishu.AutoConfigApplyResult `json:"result"`
}

type feishuAppAutoConfigPublishRequest struct {
	Remark    string `json:"remark,omitempty"`
	Changelog string `json:"changelog,omitempty"`
	Version   string `json:"version,omitempty"`
}

type feishuAppAutoConfigPublishResponse struct {
	App    adminFeishuAppSummary          `json:"app"`
	Result feishu.AutoConfigPublishResult `json:"result"`
}

type feishuAppPermissionCheckItem struct {
	Scope     string `json:"scope"`
	ScopeType string `json:"scopeType,omitempty"`
}

type feishuAppTestStartResponse struct {
	GatewayID string    `json:"gatewayId"`
	StartedAt time.Time `json:"startedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Phrase    string    `json:"phrase,omitempty"`
	Message   string    `json:"message"`
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

type feishuAppTestKind string

const (
	feishuAppTestKindEventSubscription feishuAppTestKind = "event_subscription"
	feishuAppTestKindCallback          feishuAppTestKind = "callback"

	feishuAppTestStatusPending = "pending"
	feishuAppTestStatusPassed  = "passed"
	feishuAppTestStatusExpired = "expired"

	defaultFeishuAppEventTestPhrase = "测试"
	defaultFeishuAppTestTTL         = 10 * time.Minute
	defaultFeishuAppTestSendTimeout = 10 * time.Second
)

type feishuAppTestContext struct {
	ID        string
	GatewayID string
	Kind      feishuAppTestKind
	Phrase    string
	Recipient feishuAppWebTestRecipient
	Status    string
	StartedAt time.Time
	ExpiresAt time.Time
}

type feishuAppWebTestRecipient struct {
	GatewayID     string
	ActorUserID   string
	ReceiveID     string
	ReceiveIDType string
	BoundAt       time.Time
}
