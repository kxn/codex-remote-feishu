package feishu

import (
	"context"
	"errors"
	"sort"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"

	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

const (
	autoConfigBlockingReadFailed      = "feishu_read_failed"
	autoConfigBlockingCredentialIssue = "credential_invalid"
	autoConfigBlockingPermissionIssue = "permission_denied"
)

var (
	autoConfigGetApplication        = getApplicationConfig
	autoConfigGetApplicationVersion = getApplicationVersion
)

type autoConfigService struct {
	client   *SetupClient
	manifest feishuapp.Manifest
	policy   feishuapp.FixedPolicy
}

type autoConfigSnapshot struct {
	app            *larkapplication.Application
	onlineVersion  *larkapplication.ApplicationAppVersion
	unauditVersion *larkapplication.ApplicationAppVersion
	activeVersion  *larkapplication.ApplicationAppVersion
}

func PlanAppAutoConfig(ctx context.Context, cfg LiveGatewayConfig, manifest feishuapp.Manifest, policy feishuapp.FixedPolicy) (AutoConfigPlan, error) {
	return NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).PlanAppAutoConfig(ctx, manifest, policy)
}

func (c *SetupClient) PlanAppAutoConfig(ctx context.Context, manifest feishuapp.Manifest, policy feishuapp.FixedPolicy) (AutoConfigPlan, error) {
	return newAutoConfigService(c, manifest, policy).Plan(ctx)
}

func newAutoConfigService(client *SetupClient, manifest feishuapp.Manifest, policy feishuapp.FixedPolicy) *autoConfigService {
	if manifest.Scopes.Scopes.Tenant == nil && manifest.Scopes.Scopes.User == nil && len(manifest.ScopeRequirements) == 0 && len(manifest.Events) == 0 && len(manifest.Callbacks) == 0 {
		manifest = feishuapp.DefaultManifest()
	}
	if strings.TrimSpace(policy.EventSubscriptionType) == "" {
		policy = feishuapp.DefaultFixedPolicy()
	}
	return &autoConfigService{
		client:   client,
		manifest: manifest,
		policy:   policy,
	}
}

func (s *autoConfigService) Plan(ctx context.Context) (AutoConfigPlan, error) {
	snapshot, err := s.readSnapshot(ctx)
	if err != nil {
		if plan, ok := s.planFromReadError(err); ok {
			return plan, nil
		}
		return AutoConfigPlan{}, err
	}
	return s.buildPlan(snapshot), nil
}

func (s *autoConfigService) readSnapshot(ctx context.Context) (autoConfigSnapshot, error) {
	sdkClient, broker := s.client.sdk()
	cfg := s.client.liveGatewayConfig()
	app, err := autoConfigGetApplication(ctx, broker, sdkClient, cfg.AppID)
	if err != nil {
		return autoConfigSnapshot{}, err
	}
	snapshot := autoConfigSnapshot{
		app: app,
	}
	if app == nil {
		return snapshot, nil
	}
	if versionID := strings.TrimSpace(stringValue(app.OnlineVersionId)); versionID != "" {
		snapshot.onlineVersion, err = autoConfigGetApplicationVersion(ctx, broker, sdkClient, cfg.AppID, versionID)
		if err != nil {
			return autoConfigSnapshot{}, err
		}
	}
	if versionID := strings.TrimSpace(stringValue(app.UnauditVersionId)); versionID != "" {
		snapshot.unauditVersion, err = autoConfigGetApplicationVersion(ctx, broker, sdkClient, cfg.AppID, versionID)
		if err != nil {
			return autoConfigSnapshot{}, err
		}
	}
	if snapshot.unauditVersion != nil {
		snapshot.activeVersion = snapshot.unauditVersion
	} else {
		snapshot.activeVersion = snapshot.onlineVersion
	}
	return snapshot, nil
}

func (s *autoConfigService) buildPlan(snapshot autoConfigSnapshot) AutoConfigPlan {
	configuredScopes := configuredScopeRefs(snapshot.app)
	configuredEvents := sortUniqueStrings(appSubscribedEvents(snapshot.app))
	configuredCallbacks := sortUniqueStrings(appSubscribedCallbacks(snapshot.app))
	targetScopes := normalizeScopeRequirements(s.manifest)
	targetScopeRefs := scopeRefsFromRequirements(targetScopes)
	targetEventKeys := eventKeys(s.manifest.Events)
	targetCallbackKeys := callbackKeys(s.manifest.Callbacks)

	diff := AutoConfigDiff{
		MissingScopes:                 subtractScopeRefs(targetScopeRefs, configuredScopes),
		ExtraScopes:                   subtractScopeRefs(configuredScopes, targetScopeRefs),
		MissingEvents:                 subtractStrings(targetEventKeys, configuredEvents),
		ExtraEvents:                   subtractStrings(configuredEvents, targetEventKeys),
		MissingCallbacks:              subtractStrings(targetCallbackKeys, configuredCallbacks),
		ExtraCallbacks:                subtractStrings(configuredCallbacks, targetCallbackKeys),
		EventSubscriptionTypeMismatch: strings.TrimSpace(stringValue(subscribedEventField(snapshot.app, "type"))) != s.policy.EventSubscriptionType,
		EventRequestURLMismatch:       strings.TrimSpace(stringValue(subscribedEventField(snapshot.app, "url"))) != s.policy.EventRequestURL,
		CallbackTypeMismatch:          strings.TrimSpace(stringValue(callbackField(snapshot.app, "type"))) != s.policy.CallbackType,
		CallbackRequestURLMismatch:    strings.TrimSpace(stringValue(callbackField(snapshot.app, "url"))) != s.policy.CallbackRequestURL,
	}
	diff.ConfigPatchRequired = len(diff.MissingScopes) > 0 ||
		len(diff.ExtraScopes) > 0 ||
		len(diff.MissingEvents) > 0 ||
		len(diff.ExtraEvents) > 0 ||
		len(diff.MissingCallbacks) > 0 ||
		len(diff.ExtraCallbacks) > 0 ||
		diff.EventSubscriptionTypeMismatch ||
		diff.EventRequestURLMismatch ||
		diff.CallbackTypeMismatch ||
		diff.CallbackRequestURLMismatch
	diff.AbilityPatchRequired = observedBotEnabled(snapshot.activeVersion) != s.policy.BotEnabled

	publishState := buildPublishState(snapshot, diff)
	diff.PublishRequired = publishState.NeedsPublish

	plan := AutoConfigPlan{
		Current: AutoConfigObservedState{
			ConfiguredScopes:            configuredScopes,
			EventSubscriptionType:       strings.TrimSpace(stringValue(subscribedEventField(snapshot.app, "type"))),
			EventRequestURL:             strings.TrimSpace(stringValue(subscribedEventField(snapshot.app, "url"))),
			ConfiguredEvents:            configuredEvents,
			CallbackType:                strings.TrimSpace(stringValue(callbackField(snapshot.app, "type"))),
			CallbackRequestURL:          strings.TrimSpace(stringValue(callbackField(snapshot.app, "url"))),
			ConfiguredCallbacks:         configuredCallbacks,
			OnlineVersionID:             versionID(snapshot.onlineVersion),
			OnlineVersion:               versionString(snapshot.onlineVersion),
			OnlineVersionStatus:         versionStatusLabel(snapshot.onlineVersion),
			UnauditVersionID:            versionID(snapshot.unauditVersion),
			UnauditVersion:              versionString(snapshot.unauditVersion),
			UnauditVersionStatus:        versionStatusLabel(snapshot.unauditVersion),
			ActiveVersionID:             versionID(snapshot.activeVersion),
			ActiveVersion:               versionString(snapshot.activeVersion),
			ActiveVersionStatus:         versionStatusLabel(snapshot.activeVersion),
			ActiveVersionEvents:         activeVersionEvents(snapshot.activeVersion),
			BotEnabled:                  observedBotEnabled(snapshot.activeVersion),
			MessageCardCallbackURL:      strings.TrimSpace(observedCardCallbackURL(snapshot.activeVersion)),
			MobileDefaultAbility:        appDefaultAbility(snapshot.app, "mobile"),
			PcDefaultAbility:            appDefaultAbility(snapshot.app, "pc"),
			EncryptionKeyConfigured:     strings.TrimSpace(encryptionField(snapshot.app, "key")) != "",
			VerificationTokenConfigured: strings.TrimSpace(encryptionField(snapshot.app, "token")) != "",
		},
		Target: AutoConfigTargetState{
			ScopeRequirements: targetScopes,
			Events:            append([]feishuapp.EventRequirement(nil), s.manifest.Events...),
			Callbacks:         append([]feishuapp.CallbackRequirement(nil), s.manifest.Callbacks...),
			Policy:            s.policy,
		},
		Diff:    diff,
		Publish: publishState,
	}
	plan.BlockingRequirements, plan.DegradableRequirements = s.buildRequirementStatus(targetScopes, configuredEvents, configuredCallbacks, configuredScopes)
	plan.Status, plan.Summary = derivePlanState(plan)
	return plan
}

func (s *autoConfigService) planFromReadError(err error) (AutoConfigPlan, bool) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr == nil {
		return AutoConfigPlan{}, false
	}
	plan := AutoConfigPlan{
		Target: AutoConfigTargetState{
			ScopeRequirements: normalizeScopeRequirements(s.manifest),
			Events:            append([]feishuapp.EventRequirement(nil), s.manifest.Events...),
			Callbacks:         append([]feishuapp.CallbackRequirement(nil), s.manifest.Callbacks...),
			Policy:            s.policy,
		},
	}
	plan = overridePlanFromAPIError(plan, err)
	switch plan.Status {
	case AutoConfigStatusBlocked:
		return plan, true
	default:
		return AutoConfigPlan{}, false
	}
}

func (s *autoConfigService) buildRequirementStatus(scopeReqs []feishuapp.ScopeRequirement, configuredEvents []string, configuredCallbacks []string, configuredScopes []AutoConfigScopeRef) ([]AutoConfigRequirementStatus, []AutoConfigRequirementStatus) {
	configuredScopeKeys := scopeRefMap(configuredScopes)
	eventKeys := stringSet(configuredEvents)
	callbackKeys := stringSet(configuredCallbacks)
	var blocking []AutoConfigRequirementStatus
	var degradable []AutoConfigRequirementStatus

	appendRequirement := func(item AutoConfigRequirementStatus) {
		if item.Required {
			blocking = append(blocking, item)
			return
		}
		degradable = append(degradable, item)
	}

	for _, item := range scopeReqs {
		status := AutoConfigRequirementStatus{
			Kind:           AutoConfigRequirementKindScope,
			Key:            strings.TrimSpace(item.Scope),
			ScopeType:      strings.TrimSpace(item.ScopeType),
			Feature:        strings.TrimSpace(item.Feature),
			Required:       item.Required,
			DegradeMessage: strings.TrimSpace(item.DegradeMessage),
			Present:        configuredScopeKeys[scopeKey(item.Scope, item.ScopeType)],
		}
		if status.Present {
			continue
		}
		appendRequirement(status)
	}
	for _, item := range s.manifest.Events {
		status := AutoConfigRequirementStatus{
			Kind:           AutoConfigRequirementKindEvent,
			Key:            strings.TrimSpace(item.Event),
			Feature:        strings.TrimSpace(item.Feature),
			Purpose:        strings.TrimSpace(item.Purpose),
			Required:       item.Required,
			DegradeMessage: strings.TrimSpace(item.DegradeMessage),
			Present:        eventKeys[strings.TrimSpace(item.Event)],
		}
		if status.Present {
			continue
		}
		appendRequirement(status)
	}
	for _, item := range s.manifest.Callbacks {
		status := AutoConfigRequirementStatus{
			Kind:           AutoConfigRequirementKindCallback,
			Key:            strings.TrimSpace(item.Callback),
			Feature:        strings.TrimSpace(item.Feature),
			Purpose:        strings.TrimSpace(item.Purpose),
			Required:       item.Required,
			DegradeMessage: strings.TrimSpace(item.DegradeMessage),
			Present:        callbackKeys[strings.TrimSpace(item.Callback)],
		}
		if status.Present {
			continue
		}
		appendRequirement(status)
	}
	sort.Slice(blocking, func(i, j int) bool { return blocking[i].Kind+blocking[i].Key < blocking[j].Kind+blocking[j].Key })
	sort.Slice(degradable, func(i, j int) bool {
		return degradable[i].Kind+degradable[i].Key < degradable[j].Kind+degradable[j].Key
	})
	return blocking, degradable
}

func buildPublishState(snapshot autoConfigSnapshot, diff AutoConfigDiff) AutoConfigPublishState {
	state := AutoConfigPublishState{
		OnlineVersionID:      versionID(snapshot.onlineVersion),
		OnlineVersion:        versionString(snapshot.onlineVersion),
		OnlineVersionStatus:  versionStatusLabel(snapshot.onlineVersion),
		UnauditVersionID:     versionID(snapshot.unauditVersion),
		UnauditVersion:       versionString(snapshot.unauditVersion),
		UnauditVersionStatus: versionStatusLabel(snapshot.unauditVersion),
		ActiveVersionID:      versionID(snapshot.activeVersion),
		ActiveVersion:        versionString(snapshot.activeVersion),
		ActiveVersionStatus:  versionStatusLabel(snapshot.activeVersion),
	}
	unauditStatus := versionStatusLabel(snapshot.unauditVersion)
	state.AwaitingReview = unauditStatus == "under_audit"
	state.NeedsPublish = diff.ConfigPatchRequired || diff.AbilityPatchRequired
	if !state.NeedsPublish {
		switch {
		case snapshot.onlineVersion == nil:
			state.NeedsPublish = true
		case snapshot.unauditVersion != nil && unauditStatus != "under_audit" && unauditStatus != "":
			state.NeedsPublish = true
		}
	}
	if state.AwaitingReview {
		state.NeedsPublish = false
	}
	return state
}

func derivePlanState(plan AutoConfigPlan) (string, string) {
	switch {
	case plan.Publish.AwaitingReview:
		return AutoConfigStatusAwaitingReview, "飞书应用变更已进入审核流程，正在等待审核结果。"
	case plan.Diff.ConfigPatchRequired || plan.Diff.AbilityPatchRequired:
		return AutoConfigStatusApplyRequired, "存在尚未补齐的飞书配置差异。"
	case len(plan.BlockingRequirements) > 0:
		return AutoConfigStatusBlocked, "仍缺少阻塞性的飞书配置项，当前不能宣称机器人已可正常使用。"
	case len(plan.DegradableRequirements) > 0:
		return AutoConfigStatusDegraded, "飞书应用已可用，但仍有可降级缺失项。"
	default:
		return AutoConfigStatusClean, "飞书应用配置已收敛。"
	}
}

func getApplicationConfig(ctx context.Context, broker *FeishuCallBroker, client *lark.Client, appID string) (*larkapplication.Application, error) {
	resp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "application.v6.application.get",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityReadAssist,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkapplication.GetApplicationResp, error) {
		req := larkapplication.NewGetApplicationReqBuilder().
			AppId(strings.TrimSpace(appID)).
			Lang("zh_cn").
			Build()
		return sdkClient.Application.V6.Application.Get(callCtx, req)
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("application.v6.application.get", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return nil, nil
	}
	return resp.Data.App, nil
}

func getApplicationVersion(ctx context.Context, broker *FeishuCallBroker, client *lark.Client, appID, versionID string) (*larkapplication.ApplicationAppVersion, error) {
	resp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "application.v6.application_app_version.get",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityReadAssist,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkapplication.GetApplicationAppVersionResp, error) {
		req := larkapplication.NewGetApplicationAppVersionReqBuilder().
			AppId(strings.TrimSpace(appID)).
			VersionId(strings.TrimSpace(versionID)).
			Lang("zh_cn").
			Build()
		return sdkClient.Application.V6.ApplicationAppVersion.Get(callCtx, req)
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("application.v6.application_app_version.get", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return nil, nil
	}
	return resp.Data.AppVersion, nil
}

func normalizeScopeRequirements(manifest feishuapp.Manifest) []feishuapp.ScopeRequirement {
	if len(manifest.ScopeRequirements) > 0 {
		out := make([]feishuapp.ScopeRequirement, 0, len(manifest.ScopeRequirements))
		for _, item := range manifest.ScopeRequirements {
			item.Scope = strings.TrimSpace(item.Scope)
			item.ScopeType = normalizeTokenType(item.ScopeType)
			if item.Scope == "" {
				continue
			}
			out = append(out, item)
		}
		return out
	}
	var out []feishuapp.ScopeRequirement
	for _, item := range manifest.Scopes.Scopes.Tenant {
		scope := strings.TrimSpace(item)
		if scope == "" {
			continue
		}
		out = append(out, feishuapp.ScopeRequirement{Scope: scope, ScopeType: "tenant", Required: true})
	}
	for _, item := range manifest.Scopes.Scopes.User {
		scope := strings.TrimSpace(item)
		if scope == "" {
			continue
		}
		out = append(out, feishuapp.ScopeRequirement{Scope: scope, ScopeType: "user", Required: true})
	}
	return out
}

func scopeRefsFromRequirements(values []feishuapp.ScopeRequirement) []AutoConfigScopeRef {
	out := make([]AutoConfigScopeRef, 0, len(values))
	for _, item := range values {
		scope := strings.TrimSpace(item.Scope)
		if scope == "" {
			continue
		}
		out = append(out, AutoConfigScopeRef{Scope: scope, ScopeType: normalizeTokenType(item.ScopeType)})
	}
	return sortScopeRefs(out)
}

func configuredScopeRefs(app *larkapplication.Application) []AutoConfigScopeRef {
	if app == nil {
		return nil
	}
	var out []AutoConfigScopeRef
	for _, item := range app.Scopes {
		if item == nil {
			continue
		}
		scope := strings.TrimSpace(stringValue(item.Scope))
		if scope == "" {
			continue
		}
		if len(item.TokenTypes) == 0 {
			out = append(out, AutoConfigScopeRef{Scope: scope})
			continue
		}
		for _, tokenType := range item.TokenTypes {
			out = append(out, AutoConfigScopeRef{
				Scope:     scope,
				ScopeType: normalizeTokenType(tokenType),
			})
		}
	}
	return sortScopeRefs(out)
}

func appSubscribedEvents(app *larkapplication.Application) []string {
	if app == nil || app.Event == nil {
		return nil
	}
	return append([]string(nil), app.Event.SubscribedEvents...)
}

func appSubscribedCallbacks(app *larkapplication.Application) []string {
	if app == nil || app.Callback == nil {
		return nil
	}
	return append([]string(nil), app.Callback.SubscribedCallbacks...)
}

func activeVersionEvents(version *larkapplication.ApplicationAppVersion) []string {
	if version == nil {
		return nil
	}
	if len(version.Events) > 0 {
		return sortUniqueStrings(version.Events)
	}
	out := make([]string, 0, len(version.EventInfos))
	for _, item := range version.EventInfos {
		if item == nil {
			continue
		}
		if key := strings.TrimSpace(stringValue(item.EventType)); key != "" {
			out = append(out, key)
		}
	}
	return sortUniqueStrings(out)
}

func observedBotEnabled(version *larkapplication.ApplicationAppVersion) bool {
	return version != nil && version.Ability != nil && version.Ability.Bot != nil
}

func observedCardCallbackURL(version *larkapplication.ApplicationAppVersion) string {
	if version == nil || version.Ability == nil || version.Ability.Bot == nil {
		return ""
	}
	return stringValue(version.Ability.Bot.CardRequestUrl)
}

func versionID(version *larkapplication.ApplicationAppVersion) string {
	if version == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(version.VersionId))
}

func versionString(version *larkapplication.ApplicationAppVersion) string {
	if version == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(version.Version))
}

func versionStatusLabel(version *larkapplication.ApplicationAppVersion) string {
	if version == nil || version.Status == nil {
		return ""
	}
	switch *version.Status {
	case larkapplication.AppVersionStatusAudited:
		return "audited"
	case larkapplication.AppVersionStatusReject:
		return "rejected"
	case larkapplication.AppVersionStatusUnderAudit:
		return "under_audit"
	case larkapplication.AppVersionStatusUnaudit:
		return "unaudit"
	default:
		return "unknown"
	}
}

func subscribedEventField(app *larkapplication.Application, field string) *string {
	if app == nil || app.Event == nil {
		return nil
	}
	switch field {
	case "type":
		return app.Event.SubscriptionType
	case "url":
		return app.Event.RequestUrl
	default:
		return nil
	}
}

func callbackField(app *larkapplication.Application, field string) *string {
	if app == nil || app.Callback == nil {
		return nil
	}
	switch field {
	case "type":
		return app.Callback.CallbackType
	case "url":
		return app.Callback.RequestUrl
	default:
		return nil
	}
}

func encryptionField(app *larkapplication.Application, field string) string {
	if app == nil || app.Encryption == nil {
		return ""
	}
	switch field {
	case "key":
		return stringValue(app.Encryption.EncryptionKey)
	case "token":
		return stringValue(app.Encryption.VerificationToken)
	default:
		return ""
	}
}

func appDefaultAbility(app *larkapplication.Application, kind string) string {
	if app == nil {
		return ""
	}
	switch kind {
	case "mobile":
		return strings.TrimSpace(stringValue(app.MobileDefaultAbility))
	case "pc":
		return strings.TrimSpace(stringValue(app.PcDefaultAbility))
	default:
		return ""
	}
}

func eventKeys(values []feishuapp.EventRequirement) []string {
	out := make([]string, 0, len(values))
	for _, item := range values {
		if key := strings.TrimSpace(item.Event); key != "" {
			out = append(out, key)
		}
	}
	return sortUniqueStrings(out)
}

func callbackKeys(values []feishuapp.CallbackRequirement) []string {
	out := make([]string, 0, len(values))
	for _, item := range values {
		if key := strings.TrimSpace(item.Callback); key != "" {
			out = append(out, key)
		}
	}
	return sortUniqueStrings(out)
}

func scopeKey(scope, scopeType string) string {
	return strings.TrimSpace(scope) + "|" + normalizeTokenType(scopeType)
}

func scopeRefMap(values []AutoConfigScopeRef) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, item := range values {
		out[scopeKey(item.Scope, item.ScopeType)] = true
	}
	return out
}

func subtractScopeRefs(left, right []AutoConfigScopeRef) []AutoConfigScopeRef {
	rightKeys := scopeRefMap(right)
	var out []AutoConfigScopeRef
	for _, item := range left {
		if rightKeys[scopeKey(item.Scope, item.ScopeType)] {
			continue
		}
		out = append(out, item)
	}
	return sortScopeRefs(out)
}

func subtractStrings(left, right []string) []string {
	rightSet := stringSet(right)
	var out []string
	for _, item := range left {
		item = strings.TrimSpace(item)
		if item == "" || rightSet[item] {
			continue
		}
		out = append(out, item)
	}
	return sortUniqueStrings(out)
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, item := range values {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out[trimmed] = true
		}
	}
	return out
}

func sortUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, item := range values {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func sortScopeRefs(values []AutoConfigScopeRef) []AutoConfigScopeRef {
	seen := map[string]bool{}
	out := make([]AutoConfigScopeRef, 0, len(values))
	for _, item := range values {
		item.Scope = strings.TrimSpace(item.Scope)
		item.ScopeType = normalizeTokenType(item.ScopeType)
		if item.Scope == "" {
			continue
		}
		key := scopeKey(item.Scope, item.ScopeType)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ScopeType == out[j].ScopeType {
			return out[i].Scope < out[j].Scope
		}
		return out[i].ScopeType < out[j].ScopeType
	})
	return out
}

func normalizeTokenType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "user":
		return "user"
	default:
		return "tenant"
	}
}
