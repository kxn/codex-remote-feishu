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
	autoConfigBlockingUnsupported     = "unsupported_application"
	autoConfigBlockingUnderReview     = "application_under_review"
	autoConfigBlockingApplyRequired   = "apply_required_before_publish"
	autoConfigBlockingInvalidPublish  = "invalid_publish_request"
	autoConfigDefaultPublishRemark    = "同步 Codex Remote 的飞书应用配置"
	autoConfigDefaultPublishChangelog = "更新飞书自动配置所需的权限、事件、回调与机器人能力。"
)

var (
	autoConfigListScopes            = ListAppScopes
	autoConfigGetApplication        = getApplicationConfig
	autoConfigGetApplicationVersion = getApplicationVersion
	autoConfigPatchConfig           = patchV7AppConfig
	autoConfigPatchAbility          = patchV7AppAbility
	autoConfigPublish               = publishV7App
)

type autoConfigService struct {
	cfg      LiveGatewayConfig
	manifest feishuapp.Manifest
	policy   feishuapp.FixedPolicy
	client   *lark.Client
	broker   *FeishuCallBroker
}

type autoConfigSnapshot struct {
	app            *larkapplication.Application
	grantedScopes  []AppScopeStatus
	onlineVersion  *larkapplication.ApplicationAppVersion
	unauditVersion *larkapplication.ApplicationAppVersion
	activeVersion  *larkapplication.ApplicationAppVersion
}

func PlanAppAutoConfig(ctx context.Context, cfg LiveGatewayConfig, manifest feishuapp.Manifest, policy feishuapp.FixedPolicy) (AutoConfigPlan, error) {
	return newAutoConfigService(cfg, manifest, policy).Plan(ctx)
}

func ApplyAppAutoConfig(ctx context.Context, cfg LiveGatewayConfig, manifest feishuapp.Manifest, policy feishuapp.FixedPolicy) (AutoConfigApplyResult, error) {
	return newAutoConfigService(cfg, manifest, policy).Apply(ctx)
}

func PublishAppAutoConfig(ctx context.Context, cfg LiveGatewayConfig, manifest feishuapp.Manifest, policy feishuapp.FixedPolicy, req AutoConfigPublishRequest) (AutoConfigPublishResult, error) {
	return newAutoConfigService(cfg, manifest, policy).Publish(ctx, req)
}

func newAutoConfigService(cfg LiveGatewayConfig, manifest feishuapp.Manifest, policy feishuapp.FixedPolicy) *autoConfigService {
	if manifest.Scopes.Scopes.Tenant == nil && manifest.Scopes.Scopes.User == nil && len(manifest.ScopeRequirements) == 0 && len(manifest.Events) == 0 && len(manifest.Callbacks) == 0 {
		manifest = feishuapp.DefaultManifest()
	}
	if strings.TrimSpace(policy.EventSubscriptionType) == "" {
		policy = feishuapp.DefaultFixedPolicy()
	}
	client := NewLarkClient(cfg.AppID, cfg.AppSecret)
	return &autoConfigService{
		cfg:      cfg,
		manifest: manifest,
		policy:   policy,
		client:   client,
		broker:   NewFeishuCallBroker(cfg.GatewayID, client),
	}
}

func (s *autoConfigService) Plan(ctx context.Context) (AutoConfigPlan, error) {
	snapshot, err := s.readSnapshot(ctx)
	if err != nil {
		return AutoConfigPlan{}, err
	}
	return s.buildPlan(snapshot), nil
}

func (s *autoConfigService) Apply(ctx context.Context) (AutoConfigApplyResult, error) {
	plan, err := s.Plan(ctx)
	if err != nil {
		return AutoConfigApplyResult{}, err
	}
	result := AutoConfigApplyResult{
		Status:         plan.Status,
		Summary:        plan.Summary,
		BlockingReason: plan.BlockingReason,
		Plan:           plan,
	}
	if plan.Publish.AwaitingReview {
		result.Actions = append(result.Actions, AutoConfigAction{Name: "apply", Outcome: "blocked", Details: "application is under review"})
		return result, nil
	}
	if !plan.Diff.ConfigPatchRequired && !plan.Diff.AbilityPatchRequired {
		result.Actions = append(result.Actions, AutoConfigAction{Name: "apply", Outcome: "skipped", Details: "no config or ability changes required"})
		return result, nil
	}

	// Some scope updates require bot ability to be enabled first.
	if plan.Diff.AbilityPatchRequired {
		if err := autoConfigPatchAbility(ctx, s.broker, s.client, s.cfg.AppID, v7PatchAbilityRequest{
			Bot: &v7PatchAbilityBot{Enable: s.policy.BotEnabled},
		}); err != nil {
			updatedPlan := overridePlanFromAPIError(plan, err)
			return AutoConfigApplyResult{
				Status:         updatedPlan.Status,
				Summary:        updatedPlan.Summary,
				BlockingReason: updatedPlan.BlockingReason,
				Actions:        []AutoConfigAction{{Name: "ability_patch", Outcome: "blocked", Details: err.Error()}},
				Plan:           updatedPlan,
			}, nil
		}
		result.Actions = append(result.Actions, AutoConfigAction{Name: "ability_patch", Outcome: "applied"})
	}
	if plan.Diff.ConfigPatchRequired {
		req := s.buildConfigPatchRequest(plan.Diff)
		if err := autoConfigPatchConfig(ctx, s.broker, s.client, s.cfg.AppID, req); err != nil {
			updatedPlan := overridePlanFromAPIError(plan, err)
			outcome := "blocked"
			if updatedPlan.Status == AutoConfigStatusUnsupported {
				outcome = "unsupported"
			}
			actions := append([]AutoConfigAction(nil), result.Actions...)
			actions = append(actions, AutoConfigAction{Name: "config_patch", Outcome: outcome, Details: err.Error()})
			return AutoConfigApplyResult{
				Status:         updatedPlan.Status,
				Summary:        updatedPlan.Summary,
				BlockingReason: updatedPlan.BlockingReason,
				Actions:        actions,
				Plan:           updatedPlan,
			}, nil
		}
		result.Actions = append(result.Actions, AutoConfigAction{Name: "config_patch", Outcome: "applied"})
	}

	refreshed, err := s.Plan(ctx)
	if err != nil {
		return AutoConfigApplyResult{}, err
	}
	result.Status = refreshed.Status
	result.Summary = refreshed.Summary
	result.BlockingReason = refreshed.BlockingReason
	result.Plan = refreshed
	return result, nil
}

func (s *autoConfigService) Publish(ctx context.Context, req AutoConfigPublishRequest) (AutoConfigPublishResult, error) {
	plan, err := s.Plan(ctx)
	if err != nil {
		return AutoConfigPublishResult{}, err
	}
	result := AutoConfigPublishResult{
		Status:         plan.Status,
		Summary:        plan.Summary,
		BlockingReason: plan.BlockingReason,
		Plan:           plan,
	}
	if plan.Publish.AwaitingReview {
		result.Actions = append(result.Actions, AutoConfigAction{Name: "publish", Outcome: "skipped", Details: "application is already under review"})
		return result, nil
	}
	if plan.Diff.ConfigPatchRequired || plan.Diff.AbilityPatchRequired {
		blocked := plan
		blocked.Status = AutoConfigStatusBlocked
		blocked.Summary = "仍有未写入的自动配置变更，发布前需要先执行 apply。"
		blocked.BlockingReason = autoConfigBlockingApplyRequired
		result.Status = blocked.Status
		result.Summary = blocked.Summary
		result.BlockingReason = blocked.BlockingReason
		result.Plan = blocked
		result.Actions = append(result.Actions, AutoConfigAction{Name: "publish", Outcome: "blocked", Details: "apply is required before publish"})
		return result, nil
	}
	if !plan.Publish.NeedsPublish {
		result.Actions = append(result.Actions, AutoConfigAction{Name: "publish", Outcome: "skipped", Details: "no publish is required"})
		return result, nil
	}

	publishReq := v7PublishRequest{
		MobileDefaultAbility: s.policy.MobileDefaultAbility,
		PcDefaultAbility:     s.policy.PcDefaultAbility,
		Remark:               firstNonEmpty(strings.TrimSpace(req.Remark), autoConfigDefaultPublishRemark),
		Changelog:            firstNonEmpty(strings.TrimSpace(req.Changelog), autoConfigDefaultPublishChangelog),
		Version:              strings.TrimSpace(req.Version),
	}
	versionID, version, err := autoConfigPublish(ctx, s.broker, s.client, s.cfg.AppID, publishReq)
	if err != nil {
		updatedPlan := overridePlanFromAPIError(plan, err)
		outcome := "blocked"
		if updatedPlan.Status == AutoConfigStatusUnsupported {
			outcome = "unsupported"
		}
		return AutoConfigPublishResult{
			Status:         updatedPlan.Status,
			Summary:        updatedPlan.Summary,
			BlockingReason: updatedPlan.BlockingReason,
			Actions:        []AutoConfigAction{{Name: "publish", Outcome: outcome, Details: err.Error()}},
			Plan:           updatedPlan,
		}, nil
	}
	refreshed, err := s.Plan(ctx)
	if err != nil {
		return AutoConfigPublishResult{}, err
	}
	return AutoConfigPublishResult{
		Status:         refreshed.Status,
		Summary:        refreshed.Summary,
		BlockingReason: refreshed.BlockingReason,
		VersionID:      versionID,
		Version:        version,
		Actions:        []AutoConfigAction{{Name: "publish", Outcome: "submitted"}},
		Plan:           refreshed,
	}, nil
}

func (s *autoConfigService) readSnapshot(ctx context.Context) (autoConfigSnapshot, error) {
	app, err := autoConfigGetApplication(ctx, s.broker, s.client, s.cfg.AppID)
	if err != nil {
		return autoConfigSnapshot{}, err
	}
	scopes, err := autoConfigListScopes(ctx, s.cfg)
	if err != nil {
		return autoConfigSnapshot{}, err
	}
	snapshot := autoConfigSnapshot{
		app:           app,
		grantedScopes: scopes,
	}
	if app == nil {
		return snapshot, nil
	}
	if versionID := strings.TrimSpace(stringValue(app.OnlineVersionId)); versionID != "" {
		snapshot.onlineVersion, err = autoConfigGetApplicationVersion(ctx, s.broker, s.client, s.cfg.AppID, versionID)
		if err != nil {
			return autoConfigSnapshot{}, err
		}
	}
	if versionID := strings.TrimSpace(stringValue(app.UnauditVersionId)); versionID != "" {
		snapshot.unauditVersion, err = autoConfigGetApplicationVersion(ctx, s.broker, s.client, s.cfg.AppID, versionID)
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
	grantedScopes := grantedScopeRefs(snapshot.grantedScopes)
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
			GrantedScopes:               grantedScopes,
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
	plan.BlockingRequirements, plan.DegradableRequirements = s.buildRequirementStatus(targetScopes, configuredEvents, configuredCallbacks, grantedScopes)
	plan.Status, plan.Summary = derivePlanState(plan)
	return plan
}

func (s *autoConfigService) buildRequirementStatus(scopeReqs []feishuapp.ScopeRequirement, configuredEvents []string, configuredCallbacks []string, grantedScopes []AutoConfigScopeRef) ([]AutoConfigRequirementStatus, []AutoConfigRequirementStatus) {
	grantedKeys := scopeRefMap(grantedScopes)
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
			Present:        grantedKeys[scopeKey(item.Scope, item.ScopeType)],
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
	switch strings.TrimSpace(plan.BlockingReason) {
	case autoConfigBlockingUnsupported:
		return AutoConfigStatusUnsupported, "当前飞书应用不是开发者后台创建的自建应用，无法通过自动配置 API 修改。"
	case autoConfigBlockingUnderReview:
		return AutoConfigStatusAwaitingReview, "当前飞书应用正在审核中，暂时无法继续修改配置。"
	}
	switch {
	case plan.Publish.AwaitingReview:
		return AutoConfigStatusAwaitingReview, "飞书应用变更已进入审核流程，正在等待审核结果。"
	case plan.Diff.ConfigPatchRequired || plan.Diff.AbilityPatchRequired:
		return AutoConfigStatusApplyRequired, "存在待写入的飞书自动配置差异。"
	case plan.Publish.NeedsPublish:
		return AutoConfigStatusPublishRequired, "配置已收敛到待发布版本，仍需提交发布。"
	case len(plan.BlockingRequirements) > 0:
		return AutoConfigStatusBlocked, "仍缺少阻塞性的飞书配置项，当前不能宣称机器人已可正常使用。"
	case len(plan.DegradableRequirements) > 0:
		return AutoConfigStatusDegraded, "飞书应用已可用，但仍有可降级缺失项。"
	default:
		return AutoConfigStatusClean, "飞书应用配置已收敛。"
	}
}

func (s *autoConfigService) buildConfigPatchRequest(diff AutoConfigDiff) v7PatchConfigRequest {
	var req v7PatchConfigRequest
	if len(diff.MissingScopes) > 0 || len(diff.ExtraScopes) > 0 {
		scopeReq := &v7PatchConfigScope{}
		for _, item := range diff.MissingScopes {
			scopeReq.AddScopes = append(scopeReq.AddScopes, v7PatchConfigScopeItem{
				ScopeName: strings.TrimSpace(item.Scope),
				TokenType: normalizeTokenType(item.ScopeType),
			})
		}
		for _, item := range diff.ExtraScopes {
			scopeReq.RemoveScopes = append(scopeReq.RemoveScopes, v7PatchConfigScopeItem{
				ScopeName: strings.TrimSpace(item.Scope),
				TokenType: normalizeTokenType(item.ScopeType),
			})
		}
		req.Scope = scopeReq
	}
	if len(diff.MissingEvents) > 0 || len(diff.ExtraEvents) > 0 || diff.EventSubscriptionTypeMismatch || diff.EventRequestURLMismatch {
		requestURL := s.policy.EventRequestURL
		req.Event = &v7PatchConfigEvent{
			SubscriptionType: s.policy.EventSubscriptionType,
			RequestURL:       &requestURL,
			AddEvents:        append([]string(nil), diff.MissingEvents...),
			RemoveEvents:     append([]string(nil), diff.ExtraEvents...),
		}
	}
	if len(diff.MissingCallbacks) > 0 || len(diff.ExtraCallbacks) > 0 || diff.CallbackTypeMismatch || diff.CallbackRequestURLMismatch {
		requestURL := s.policy.CallbackRequestURL
		req.Callback = &v7PatchConfigCallback{
			CallbackType:    s.policy.CallbackType,
			RequestURL:      &requestURL,
			AddCallbacks:    append([]string(nil), diff.MissingCallbacks...),
			RemoveCallbacks: append([]string(nil), diff.ExtraCallbacks...),
		}
	}
	return req
}

func overridePlanFromAPIError(plan AutoConfigPlan, err error) AutoConfigPlan {
	updated := plan
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr == nil {
		updated.Status = AutoConfigStatusBlocked
		updated.Summary = "飞书自动配置调用失败。"
		updated.BlockingReason = "feishu_api_error"
		return updated
	}
	switch apiErr.Code {
	case 210040, 210020, 210302:
		updated.Status = AutoConfigStatusAwaitingReview
		updated.Summary = "飞书应用正在审核中，暂时无法继续修改或发布。"
		updated.BlockingReason = autoConfigBlockingUnderReview
	case 210043, 210035, 210021, 210015, 210001, 210034, 210014:
		updated.Status = AutoConfigStatusUnsupported
		updated.Summary = "当前飞书应用不是开发者后台创建的自建应用，无法通过自动配置 API 修改。"
		updated.BlockingReason = autoConfigBlockingUnsupported
	case 210303, 210304:
		updated.Status = AutoConfigStatusBlocked
		updated.Summary = "飞书发布请求参数无效，当前发布未被接受。"
		updated.BlockingReason = autoConfigBlockingInvalidPublish
	default:
		updated.Status = AutoConfigStatusBlocked
		updated.Summary = "飞书自动配置调用失败。"
		updated.BlockingReason = "feishu_api_error"
	}
	return updated
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
		req := larkapplication.NewGetApplicationReqBuilder().AppId(strings.TrimSpace(appID)).Build()
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

func grantedScopeRefs(values []AppScopeStatus) []AutoConfigScopeRef {
	out := make([]AutoConfigScopeRef, 0, len(values))
	for _, item := range values {
		if !scopeGranted(item) {
			continue
		}
		scope := strings.TrimSpace(item.ScopeName)
		if scope == "" {
			continue
		}
		out = append(out, AutoConfigScopeRef{
			Scope:     scope,
			ScopeType: normalizeTokenType(item.ScopeType),
		})
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
