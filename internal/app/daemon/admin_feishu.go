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
	App adminFeishuAppSummary `json:"app"`
}

type feishuAppVerifyResponse struct {
	App    adminFeishuAppSummary `json:"app"`
	Result feishu.VerifyResult   `json:"result"`
}

type feishuAppPublishCheckResponse struct {
	App    adminFeishuAppSummary `json:"app"`
	Ready  bool                  `json:"ready"`
	Issues []string              `json:"issues,omitempty"`
}

type feishuAppWriteRequest struct {
	ID        string  `json:"id,omitempty"`
	Name      *string `json:"name,omitempty"`
	AppID     *string `json:"appId,omitempty"`
	AppSecret *string `json:"appSecret,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
}

type feishuAppWizardUpdateRequest struct {
	ScopesExported     *bool `json:"scopesExported,omitempty"`
	EventsConfirmed    *bool `json:"eventsConfirmed,omitempty"`
	CallbacksConfirmed *bool `json:"callbacksConfirmed,omitempty"`
	MenusConfirmed     *bool `json:"menusConfirmed,omitempty"`
	Published          *bool `json:"published,omitempty"`
}

type adminFeishuAppWizardView struct {
	CredentialsSavedAt   *time.Time `json:"credentialsSavedAt,omitempty"`
	ConnectionVerifiedAt *time.Time `json:"connectionVerifiedAt,omitempty"`
	ScopesExportedAt     *time.Time `json:"scopesExportedAt,omitempty"`
	EventsConfirmedAt    *time.Time `json:"eventsConfirmedAt,omitempty"`
	CallbacksConfirmedAt *time.Time `json:"callbacksConfirmedAt,omitempty"`
	MenusConfirmedAt     *time.Time `json:"menusConfirmedAt,omitempty"`
	PublishedAt          *time.Time `json:"publishedAt,omitempty"`
}

type adminFeishuAppSummary struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name,omitempty"`
	AppID           string                    `json:"appId,omitempty"`
	HasSecret       bool                      `json:"hasSecret"`
	Enabled         bool                      `json:"enabled"`
	VerifiedAt      *time.Time                `json:"verifiedAt,omitempty"`
	Wizard          *adminFeishuAppWizardView `json:"wizard,omitempty"`
	Persisted       bool                      `json:"persisted"`
	RuntimeOnly     bool                      `json:"runtimeOnly,omitempty"`
	RuntimeOverride bool                      `json:"runtimeOverride,omitempty"`
	ReadOnly        bool                      `json:"readOnly,omitempty"`
	ReadOnlyReason  string                    `json:"readOnlyReason,omitempty"`
	Status          *feishu.GatewayStatus     `json:"status,omitempty"`
}
