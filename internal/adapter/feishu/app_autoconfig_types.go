package feishu

import "github.com/kxn/codex-remote-feishu/internal/feishuapp"

const (
	AutoConfigStatusClean           = "clean"
	AutoConfigStatusApplyRequired   = "apply_required"
	AutoConfigStatusPublishRequired = "publish_required"
	AutoConfigStatusAwaitingReview  = "awaiting_review"
	AutoConfigStatusDegraded        = "degraded"
	AutoConfigStatusBlocked         = "blocked"
	AutoConfigStatusUnsupported     = "unsupported"
)

const (
	AutoConfigRequirementKindScope    = "scope"
	AutoConfigRequirementKindEvent    = "event"
	AutoConfigRequirementKindCallback = "callback"
)

type AutoConfigScopeRef struct {
	Scope     string `json:"scope"`
	ScopeType string `json:"scopeType,omitempty"`
}

type AutoConfigRequirementStatus struct {
	Kind           string `json:"kind"`
	Key            string `json:"key"`
	ScopeType      string `json:"scopeType,omitempty"`
	Feature        string `json:"feature,omitempty"`
	Purpose        string `json:"purpose,omitempty"`
	Required       bool   `json:"required"`
	DegradeMessage string `json:"degradeMessage,omitempty"`
	Present        bool   `json:"present"`
}

type AutoConfigObservedState struct {
	ConfiguredScopes            []AutoConfigScopeRef `json:"configuredScopes,omitempty"`
	GrantedScopes               []AutoConfigScopeRef `json:"grantedScopes,omitempty"`
	EventSubscriptionType       string               `json:"eventSubscriptionType,omitempty"`
	EventRequestURL             string               `json:"eventRequestUrl,omitempty"`
	ConfiguredEvents            []string             `json:"configuredEvents,omitempty"`
	CallbackType                string               `json:"callbackType,omitempty"`
	CallbackRequestURL          string               `json:"callbackRequestUrl,omitempty"`
	ConfiguredCallbacks         []string             `json:"configuredCallbacks,omitempty"`
	OnlineVersionID             string               `json:"onlineVersionId,omitempty"`
	OnlineVersion               string               `json:"onlineVersion,omitempty"`
	OnlineVersionStatus         string               `json:"onlineVersionStatus,omitempty"`
	UnauditVersionID            string               `json:"unauditVersionId,omitempty"`
	UnauditVersion              string               `json:"unauditVersion,omitempty"`
	UnauditVersionStatus        string               `json:"unauditVersionStatus,omitempty"`
	ActiveVersionID             string               `json:"activeVersionId,omitempty"`
	ActiveVersion               string               `json:"activeVersion,omitempty"`
	ActiveVersionStatus         string               `json:"activeVersionStatus,omitempty"`
	ActiveVersionEvents         []string             `json:"activeVersionEvents,omitempty"`
	BotEnabled                  bool                 `json:"botEnabled"`
	MessageCardCallbackURL      string               `json:"messageCardCallbackUrl,omitempty"`
	MobileDefaultAbility        string               `json:"mobileDefaultAbility,omitempty"`
	PcDefaultAbility            string               `json:"pcDefaultAbility,omitempty"`
	EncryptionKeyConfigured     bool                 `json:"encryptionKeyConfigured"`
	VerificationTokenConfigured bool                 `json:"verificationTokenConfigured"`
}

type AutoConfigTargetState struct {
	ScopeRequirements []feishuapp.ScopeRequirement    `json:"scopeRequirements,omitempty"`
	Events            []feishuapp.EventRequirement    `json:"events,omitempty"`
	Callbacks         []feishuapp.CallbackRequirement `json:"callbacks,omitempty"`
	Policy            feishuapp.FixedPolicy           `json:"policy"`
}

type AutoConfigDiff struct {
	ConfigPatchRequired           bool                 `json:"configPatchRequired"`
	AbilityPatchRequired          bool                 `json:"abilityPatchRequired"`
	MissingScopes                 []AutoConfigScopeRef `json:"missingScopes,omitempty"`
	ExtraScopes                   []AutoConfigScopeRef `json:"extraScopes,omitempty"`
	MissingEvents                 []string             `json:"missingEvents,omitempty"`
	ExtraEvents                   []string             `json:"extraEvents,omitempty"`
	MissingCallbacks              []string             `json:"missingCallbacks,omitempty"`
	ExtraCallbacks                []string             `json:"extraCallbacks,omitempty"`
	EventSubscriptionTypeMismatch bool                 `json:"eventSubscriptionTypeMismatch"`
	EventRequestURLMismatch       bool                 `json:"eventRequestUrlMismatch"`
	CallbackTypeMismatch          bool                 `json:"callbackTypeMismatch"`
	CallbackRequestURLMismatch    bool                 `json:"callbackRequestUrlMismatch"`
	PublishRequired               bool                 `json:"publishRequired"`
}

type AutoConfigPublishState struct {
	OnlineVersionID      string `json:"onlineVersionId,omitempty"`
	OnlineVersion        string `json:"onlineVersion,omitempty"`
	OnlineVersionStatus  string `json:"onlineVersionStatus,omitempty"`
	UnauditVersionID     string `json:"unauditVersionId,omitempty"`
	UnauditVersion       string `json:"unauditVersion,omitempty"`
	UnauditVersionStatus string `json:"unauditVersionStatus,omitempty"`
	ActiveVersionID      string `json:"activeVersionId,omitempty"`
	ActiveVersion        string `json:"activeVersion,omitempty"`
	ActiveVersionStatus  string `json:"activeVersionStatus,omitempty"`
	NeedsPublish         bool   `json:"needsPublish"`
	AwaitingReview       bool   `json:"awaitingReview"`
}

type AutoConfigPlan struct {
	Status                 string                        `json:"status"`
	Summary                string                        `json:"summary,omitempty"`
	BlockingReason         string                        `json:"blockingReason,omitempty"`
	BlockingRequirements   []AutoConfigRequirementStatus `json:"blockingRequirements,omitempty"`
	DegradableRequirements []AutoConfigRequirementStatus `json:"degradableRequirements,omitempty"`
	Current                AutoConfigObservedState       `json:"current"`
	Target                 AutoConfigTargetState         `json:"target"`
	Diff                   AutoConfigDiff                `json:"diff"`
	Publish                AutoConfigPublishState        `json:"publish"`
}

type AutoConfigAction struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Details string `json:"details,omitempty"`
}

type AutoConfigApplyResult struct {
	Status         string             `json:"status"`
	Summary        string             `json:"summary,omitempty"`
	BlockingReason string             `json:"blockingReason,omitempty"`
	Actions        []AutoConfigAction `json:"actions,omitempty"`
	Plan           AutoConfigPlan     `json:"plan"`
}

type AutoConfigPublishRequest struct {
	Remark    string `json:"remark,omitempty"`
	Changelog string `json:"changelog,omitempty"`
	Version   string `json:"version,omitempty"`
}

type AutoConfigPublishResult struct {
	Status         string             `json:"status"`
	Summary        string             `json:"summary,omitempty"`
	BlockingReason string             `json:"blockingReason,omitempty"`
	VersionID      string             `json:"versionId,omitempty"`
	Version        string             `json:"version,omitempty"`
	Actions        []AutoConfigAction `json:"actions,omitempty"`
	Plan           AutoConfigPlan     `json:"plan"`
}
