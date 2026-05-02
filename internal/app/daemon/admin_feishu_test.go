package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type fakeAdminGatewayController struct {
	statuses      []feishu.GatewayStatus
	upserted      []feishu.GatewayAppConfig
	removed       []string
	verifyConfigs []feishu.GatewayAppConfig
	applied       []feishu.Operation
	verifyResult  feishu.VerifyResult
	verifyErr     error
	upsertErrs    []error
	removeErrs    []error
	applyErr      error
	onUpsert      func(feishu.GatewayAppConfig)
}

func (f *fakeAdminGatewayController) Start(context.Context, feishu.ActionHandler) error { return nil }
func (f *fakeAdminGatewayController) Apply(_ context.Context, operations []feishu.Operation) error {
	f.applied = append(f.applied, operations...)
	return f.applyErr
}
func (f *fakeAdminGatewayController) UpsertApp(_ context.Context, cfg feishu.GatewayAppConfig) error {
	f.upserted = append(f.upserted, cfg)
	if f.onUpsert != nil {
		f.onUpsert(cfg)
	}
	if len(f.upsertErrs) > 0 {
		err := f.upsertErrs[0]
		f.upsertErrs = f.upsertErrs[1:]
		if err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeAdminGatewayController) RemoveApp(_ context.Context, gatewayID string) error {
	f.removed = append(f.removed, gatewayID)
	if len(f.removeErrs) > 0 {
		err := f.removeErrs[0]
		f.removeErrs = f.removeErrs[1:]
		if err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeAdminGatewayController) Verify(_ context.Context, cfg feishu.GatewayAppConfig) (feishu.VerifyResult, error) {
	f.verifyConfigs = append(f.verifyConfigs, cfg)
	if f.verifyResult == (feishu.VerifyResult{}) {
		f.verifyResult = feishu.VerifyResult{Connected: true}
	}
	return f.verifyResult, f.verifyErr
}
func (f *fakeAdminGatewayController) Status() []feishu.GatewayStatus {
	return append([]feishu.GatewayStatus(nil), f.statuses...)
}

type fakeFeishuSetupClient struct {
	startResult    feishuRegistrationStartResult
	startErr       error
	pollResults    []feishuRegistrationPollResult
	pollErr        error
	pollErrs       []error
	describeResult feishuAppIdentity
	describeErr    error
	pollCalls      int
	describeCalls  int
}

func (f *fakeFeishuSetupClient) StartRegistration(context.Context) (feishuRegistrationStartResult, error) {
	return f.startResult, f.startErr
}

func (f *fakeFeishuSetupClient) PollRegistration(context.Context, string) (feishuRegistrationPollResult, error) {
	if len(f.pollErrs) > 0 {
		err := f.pollErrs[0]
		f.pollErrs = f.pollErrs[1:]
		if err != nil {
			return feishuRegistrationPollResult{}, err
		}
	}
	if f.pollErr != nil {
		return feishuRegistrationPollResult{}, f.pollErr
	}
	index := f.pollCalls
	f.pollCalls++
	if len(f.pollResults) == 0 {
		return feishuRegistrationPollResult{Status: feishuOnboardingStatusPending}, nil
	}
	if index >= len(f.pollResults) {
		index = len(f.pollResults) - 1
	}
	return f.pollResults[index], nil
}

func (f *fakeFeishuSetupClient) DescribeApp(context.Context, string, string) (feishuAppIdentity, error) {
	f.describeCalls++
	return f.describeResult, f.describeErr
}

func TestFeishuManifestRoute(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/feishu/manifest", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var manifestResp feishuManifestResponse
	if err := json.NewDecoder(rec.Body).Decode(&manifestResp); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifestResp.Manifest.Scopes.Scopes.Tenant[0] != "drive:drive" {
		t.Fatalf("unexpected manifest scopes: %#v", manifestResp.Manifest.Scopes)
	}
	if len(manifestResp.Manifest.Menus) != 7 {
		t.Fatalf("unexpected manifest menus: %#v", manifestResp.Manifest.Menus)
	}
	wantMenuKeys := []string{"menu", "stop", "steerall", "new", "reasoning", "model", "access"}
	for index, want := range wantMenuKeys {
		got := manifestResp.Manifest.Menus[index].Key
		if got != want {
			t.Fatalf("manifest menu[%d] key = %q, want %q", index, got, want)
		}
	}
}

func TestAdminFeishuPermissionCheckReturnsMissingScopesAndGrantJSON(t *testing.T) {
	oldListScopes := listFeishuAppScopes
	listFeishuAppScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return []feishu.AppScopeStatus{
			{ScopeName: "im:message", ScopeType: "tenant", GrantStatus: 1},
			{ScopeName: "im:message:send_as_bot", ScopeType: "tenant", GrantStatus: 1},
		}, nil
	}
	t.Cleanup(func() {
		listFeishuAppScopes = oldListScopes
	})

	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/apps/main/permission-check", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("permission-check status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var resp feishuAppPermissionCheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode permission-check: %v", err)
	}
	if resp.Ready {
		t.Fatalf("expected missing scopes, got ready response %#v", resp)
	}
	if resp.App.ConsoleLinks.Auth != "https://open.feishu.cn/app/cli_xxx/auth" {
		t.Fatalf("unexpected auth url: %#v", resp)
	}
	if resp.App.ConsoleLinks.Events != "https://open.feishu.cn/app/cli_xxx/event?tab=event" {
		t.Fatalf("unexpected events url: %#v", resp)
	}
	if resp.LastCheckedAt == nil {
		t.Fatalf("expected lastCheckedAt to be set: %#v", resp)
	}
	if len(resp.MissingScopes) == 0 {
		t.Fatalf("expected missing scopes to be reported: %#v", resp)
	}
	if !strings.Contains(resp.GrantJSON, "\"drive:drive\"") {
		t.Fatalf("expected grant json to include drive scope, got %s", resp.GrantJSON)
	}
}

func TestAdminFeishuEventSubscriptionTestStartAndPass(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	gateway := &fakeAdminGatewayController{}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")
	app.bindFeishuAppWebTestRecipient("main", "user-1")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/test-events", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("test-events status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var resp feishuAppTestStartResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode test-events: %v", err)
	}
	if resp.Phrase != defaultFeishuAppEventTestPhrase {
		t.Fatalf("unexpected event test phrase: %#v", resp)
	}
	if len(gateway.applied) != 1 {
		t.Fatalf("expected one outgoing test prompt, got %#v", gateway.applied)
	}
	if gateway.applied[0].Kind != feishu.OperationSendCard || gateway.applied[0].CardTitle != "事件订阅测试" {
		t.Fatalf("unexpected prompt operation: %#v", gateway.applied[0])
	}
	if gateway.applied[0].AttentionUserID != "user-1" {
		t.Fatalf("expected event test card to mention recipient, got %#v", gateway.applied[0])
	}
	cardPayload, err := json.Marshal(gateway.applied[0].CardElements)
	if err != nil {
		t.Fatalf("marshal event test card: %v", err)
	}
	if !strings.Contains(string(cardPayload), defaultFeishuAppEventTestPhrase) {
		t.Fatalf("expected event test phrase in card, got %s", string(cardPayload))
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "main",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-event-pass",
		Text:             defaultFeishuAppEventTestPhrase,
	})

	if len(gateway.applied) != 2 {
		t.Fatalf("expected success reply after passing event test, got %#v", gateway.applied)
	}
	reply := gateway.applied[1]
	if reply.ReplyToMessageID != "msg-event-pass" || reply.Text != "测试成功，请回到配置页面继续下一步工作。" {
		t.Fatalf("unexpected event test reply: %#v", reply)
	}
}

func TestAdminFeishuCallbackTestStartAndPass(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	gateway := &fakeAdminGatewayController{}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")
	app.bindFeishuAppWebTestRecipient("main", "user-1")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/test-callback", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("test-callback status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if len(gateway.applied) != 1 || gateway.applied[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected one callback test card, got %#v", gateway.applied)
	}
	if gateway.applied[0].AttentionUserID != "user-1" {
		t.Fatalf("expected callback test card to mention recipient, got %#v", gateway.applied[0])
	}
	callbackPayload, err := json.Marshal(gateway.applied[0].CardElements)
	if err != nil {
		t.Fatalf("marshal callback test card: %v", err)
	}
	if !strings.Contains(string(callbackPayload), "点此测试回调") {
		t.Fatalf("expected callback button in card, got %s", string(callbackPayload))
	}
	button := gateway.applied[0].CardElements[len(gateway.applied[0].CardElements)-1]
	if button["tag"] != "button" {
		t.Fatalf("expected callback test card to use direct button element, got %#v", gateway.applied[0].CardElements)
	}
	if _, ok := button["value"].(map[string]any); ok {
		t.Fatalf("expected callback test card to avoid legacy button value payload, got %#v", button)
	}
	behaviors, _ := button["behaviors"].([]map[string]any)
	if len(behaviors) != 1 || behaviors[0]["type"] != "callback" {
		t.Fatalf("expected callback test button to use one callback behavior, got %#v", button)
	}
	value, _ := behaviors[0]["value"].(map[string]any)
	if value["kind"] != "page_action" || value["action_kind"] != string(control.ActionFeishuAppTestCallback) {
		t.Fatalf("unexpected callback test button payload: %#v", value)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionFeishuAppTestCallback,
		GatewayID:        "main",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-callback-pass",
	})

	if len(gateway.applied) != 2 {
		t.Fatalf("expected success reply after passing callback test, got %#v", gateway.applied)
	}
	reply := gateway.applied[1]
	if reply.ReplyToMessageID != "msg-callback-pass" || reply.Text != "回调测试成功，请回到配置页面继续下一步工作。" {
		t.Fatalf("unexpected callback test reply: %#v", reply)
	}
}

func TestAdminFeishuTestStartFailsWithoutBoundRecipient(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/test-events", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("test-events without recipient status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "feishu_app_web_test_recipient_unavailable") {
		t.Fatalf("expected recipient unavailable error, got %s", rec.Body.String())
	}
}

func TestSetupFeishuOnboardingSessionLifecycleCreatesAndVerifiesApp(t *testing.T) {
	cfg := config.DefaultAppConfig()
	gateway := &fakeAdminGatewayController{
		verifyResult: feishu.VerifyResult{Connected: true, Duration: time.Second},
	}
	app, configPath := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")
	app.feishuRuntime.setup = &fakeFeishuSetupClient{
		startResult: feishuRegistrationStartResult{
			DeviceCode:      "device-1",
			VerificationURL: "https://example.test/qr",
			ExpiresAt:       time.Now().Add(5 * time.Minute),
		},
		pollResults: []feishuRegistrationPollResult{{
			Status:    feishuOnboardingStatusReady,
			AppID:     "cli_qr",
			AppSecret: "secret_qr",
		}},
		describeResult: feishuAppIdentity{DisplayName: "扫码 Bot"},
	}

	createRec := performAdminRequest(t, app, http.MethodPost, "/api/setup/feishu/onboarding/sessions", "")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create onboarding status = %d, want 201 body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp feishuOnboardingSessionResponse
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode onboarding create: %v", err)
	}
	if createResp.Session.Status != feishuOnboardingStatusPending || createResp.Session.ID == "" || createResp.Session.QRCodeDataURL == "" {
		t.Fatalf("unexpected onboarding session: %#v", createResp.Session)
	}

	getRec := performAdminRequest(t, app, http.MethodGet, "/api/setup/feishu/onboarding/sessions/"+createResp.Session.ID, "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("get onboarding status = %d, want 200 body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp feishuOnboardingSessionResponse
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode onboarding get: %v", err)
	}
	if getResp.Session.Status != feishuOnboardingStatusReady || getResp.Session.AppID != "cli_qr" || getResp.Session.DisplayName != "扫码 Bot" {
		t.Fatalf("unexpected onboarding get response: %#v", getResp.Session)
	}

	completeRec := performAdminRequest(t, app, http.MethodPost, "/api/setup/feishu/onboarding/sessions/"+createResp.Session.ID+"/complete", "")
	if completeRec.Code != http.StatusOK {
		t.Fatalf("complete onboarding status = %d, want 200 body=%s", completeRec.Code, completeRec.Body.String())
	}
	var completeResp feishuOnboardingCompleteResponse
	if err := json.NewDecoder(completeRec.Body).Decode(&completeResp); err != nil {
		t.Fatalf("decode onboarding complete: %v", err)
	}
	if completeResp.App.Name != "扫码 Bot" || completeResp.App.AppID != "cli_qr" {
		t.Fatalf("unexpected completed app: %#v", completeResp.App)
	}
	if completeResp.Mutation == nil || completeResp.Mutation.Kind != "created" {
		t.Fatalf("unexpected onboarding mutation: %#v", completeResp.Mutation)
	}
	if completeResp.Guide.RecommendedNextStep != "runtimeRequirements" || len(completeResp.Guide.RemainingManualActions) == 0 {
		t.Fatalf("unexpected onboarding guide: %#v", completeResp.Guide)
	}
	if completeResp.Session.Status != feishuOnboardingStatusCompleted {
		t.Fatalf("expected completed onboarding session, got %#v", completeResp.Session)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Feishu.Apps) != 1 {
		t.Fatalf("expected one saved app, got %#v", loaded.Config.Feishu.Apps)
	}
	if loaded.Config.Feishu.Apps[0].Name != "扫码 Bot" || loaded.Config.Feishu.Apps[0].VerifiedAt == nil {
		t.Fatalf("unexpected saved app: %#v", loaded.Config.Feishu.Apps[0])
	}
}

func TestSetupFeishuOnboardingPollTransientErrorKeepsPending(t *testing.T) {
	cfg := config.DefaultAppConfig()
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	setupClient := &fakeFeishuSetupClient{
		startResult: feishuRegistrationStartResult{
			DeviceCode:      "device-poll-retry",
			VerificationURL: "https://example.test/retry-qr",
			ExpiresAt:       time.Now().Add(5 * time.Minute),
		},
		pollErrs: []error{errors.New("temporary poll timeout")},
	}
	app.feishuRuntime.setup = setupClient

	createRec := performAdminRequest(t, app, http.MethodPost, "/api/setup/feishu/onboarding/sessions", "")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create onboarding status = %d, want 201 body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp feishuOnboardingSessionResponse
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode onboarding create: %v", err)
	}

	getRec := performAdminRequest(t, app, http.MethodGet, "/api/setup/feishu/onboarding/sessions/"+createResp.Session.ID, "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("get onboarding after transient poll failure status = %d, want 200 body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp feishuOnboardingSessionResponse
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode onboarding get after transient failure: %v", err)
	}
	if getResp.Session.Status != feishuOnboardingStatusPending {
		t.Fatalf("expected onboarding session to stay pending, got %#v", getResp.Session)
	}
	if getResp.Session.ErrorCode != "" || getResp.Session.ErrorMessage != "" {
		t.Fatalf("expected transient poll failure to stay hidden from page state, got %#v", getResp.Session)
	}
}

func TestAdminFeishuOnboardingSessionLifecycleCreatesAndVerifiesApp(t *testing.T) {
	cfg := config.DefaultAppConfig()
	gateway := &fakeAdminGatewayController{
		verifyResult: feishu.VerifyResult{Connected: true, Duration: time.Second},
	}
	app, configPath := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")
	app.feishuRuntime.setup = &fakeFeishuSetupClient{
		startResult: feishuRegistrationStartResult{
			DeviceCode:      "device-admin-1",
			VerificationURL: "https://example.test/admin-qr",
			ExpiresAt:       time.Now().Add(5 * time.Minute),
		},
		pollResults: []feishuRegistrationPollResult{{
			Status:    feishuOnboardingStatusReady,
			AppID:     "cli_admin_qr",
			AppSecret: "secret_admin_qr",
		}},
		describeResult: feishuAppIdentity{DisplayName: "Admin 扫码 Bot"},
	}

	createRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/onboarding/sessions", "")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create onboarding status = %d, want 201 body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp feishuOnboardingSessionResponse
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode onboarding create: %v", err)
	}

	getRec := performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/onboarding/sessions/"+createResp.Session.ID, "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("get onboarding status = %d, want 200 body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp feishuOnboardingSessionResponse
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode onboarding get: %v", err)
	}
	if getResp.Session.Status != feishuOnboardingStatusReady || getResp.Session.AppID != "cli_admin_qr" || getResp.Session.DisplayName != "Admin 扫码 Bot" {
		t.Fatalf("unexpected onboarding get response: %#v", getResp.Session)
	}

	completeRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/onboarding/sessions/"+createResp.Session.ID+"/complete", "")
	if completeRec.Code != http.StatusOK {
		t.Fatalf("complete onboarding status = %d, want 200 body=%s", completeRec.Code, completeRec.Body.String())
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Feishu.Apps) != 1 {
		t.Fatalf("expected one saved app, got %#v", loaded.Config.Feishu.Apps)
	}
	if loaded.Config.Feishu.Apps[0].Name != "Admin 扫码 Bot" || loaded.Config.Feishu.Apps[0].VerifiedAt == nil {
		t.Fatalf("unexpected saved app: %#v", loaded.Config.Feishu.Apps[0])
	}
}

func TestSetupFeishuOnboardingRetryDoesNotDuplicateAppAfterVerifyFailure(t *testing.T) {
	cfg := config.DefaultAppConfig()
	gateway := &fakeAdminGatewayController{
		verifyResult: feishu.VerifyResult{Connected: false, ErrorCode: "verify_failed", ErrorMessage: "bot ability missing"},
		verifyErr:    errors.New("bot ability missing"),
	}
	app, configPath := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")
	app.feishuRuntime.setup = &fakeFeishuSetupClient{
		startResult: feishuRegistrationStartResult{
			DeviceCode:      "device-2",
			VerificationURL: "https://example.test/qr-2",
			ExpiresAt:       time.Now().Add(5 * time.Minute),
		},
		pollResults: []feishuRegistrationPollResult{{
			Status:    feishuOnboardingStatusReady,
			AppID:     "cli_retry",
			AppSecret: "secret_retry",
		}},
		describeResult: feishuAppIdentity{DisplayName: "Retry Bot"},
	}

	createRec := performAdminRequest(t, app, http.MethodPost, "/api/setup/feishu/onboarding/sessions", "")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create onboarding status = %d, want 201 body=%s", createRec.Code, createRec.Body.String())
	}
	var createResp feishuOnboardingSessionResponse
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode onboarding create: %v", err)
	}

	getRec := performAdminRequest(t, app, http.MethodGet, "/api/setup/feishu/onboarding/sessions/"+createResp.Session.ID, "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("get onboarding status = %d, want 200 body=%s", getRec.Code, getRec.Body.String())
	}

	completeRec := performAdminRequest(t, app, http.MethodPost, "/api/setup/feishu/onboarding/sessions/"+createResp.Session.ID+"/complete", "")
	if completeRec.Code != http.StatusBadGateway {
		t.Fatalf("first complete status = %d, want 502 body=%s", completeRec.Code, completeRec.Body.String())
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(first): %v", err)
	}
	if len(loaded.Config.Feishu.Apps) != 1 {
		t.Fatalf("expected one saved app after failed verify, got %#v", loaded.Config.Feishu.Apps)
	}

	gateway.verifyErr = nil
	gateway.verifyResult = feishu.VerifyResult{Connected: true, Duration: time.Second}
	retryRec := performAdminRequest(t, app, http.MethodPost, "/api/setup/feishu/onboarding/sessions/"+createResp.Session.ID+"/complete", "")
	if retryRec.Code != http.StatusOK {
		t.Fatalf("retry complete status = %d, want 200 body=%s", retryRec.Code, retryRec.Body.String())
	}

	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(retry): %v", err)
	}
	if len(loaded.Config.Feishu.Apps) != 1 || loaded.Config.Feishu.Apps[0].VerifiedAt == nil {
		t.Fatalf("expected retry to reuse saved app, got %#v", loaded.Config.Feishu.Apps)
	}
}

func TestFeishuAppsCreateUpdateVerifyAndDisable(t *testing.T) {
	cfg := config.DefaultAppConfig()
	gateway := &fakeAdminGatewayController{
		verifyResult: feishu.VerifyResult{Connected: true, Duration: time.Second},
	}
	app, configPath := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	var createResp feishuAppResponse
	if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResp.Mutation == nil || createResp.Mutation.Kind != "created" {
		t.Fatalf("unexpected create mutation: %#v", createResp.Mutation)
	}
	if len(gateway.upserted) != 1 || gateway.upserted[0].GatewayID != "main" {
		t.Fatalf("unexpected upserted configs: %#v", gateway.upserted)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Feishu.Apps) != 1 || loaded.Config.Feishu.Apps[0].AppSecret != "secret_xxx" {
		t.Fatalf("unexpected saved config after create: %#v", loaded.Config.Feishu.Apps)
	}

	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"name":"Main Bot 2","appSecret":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var updateResp feishuAppResponse
	if err := json.NewDecoder(rec.Body).Decode(&updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResp.Mutation == nil || updateResp.Mutation.Kind != "updated" {
		t.Fatalf("unexpected update mutation: %#v", updateResp.Mutation)
	}
	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(update): %v", err)
	}
	if loaded.Config.Feishu.Apps[0].Name != "Main Bot 2" || loaded.Config.Feishu.Apps[0].AppSecret != "secret_xxx" {
		t.Fatalf("unexpected saved config after update: %#v", loaded.Config.Feishu.Apps[0])
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/verify", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(verify): %v", err)
	}
	if loaded.Config.Feishu.Apps[0].VerifiedAt == nil {
		t.Fatalf("expected verifiedAt to be persisted, got %#v", loaded.Config.Feishu.Apps[0])
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/disable", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(disable): %v", err)
	}
	if loaded.Config.Feishu.Apps[0].Enabled == nil || *loaded.Config.Feishu.Apps[0].Enabled {
		t.Fatalf("expected app disabled, got %#v", loaded.Config.Feishu.Apps[0].Enabled)
	}
	if len(gateway.upserted) < 3 || gateway.upserted[len(gateway.upserted)-1].Enabled {
		t.Fatalf("expected disable to hot-apply runtime config, got %#v", gateway.upserted)
	}
}

func TestFeishuCreateAutoFillsDisplayNameWhenNameOmitted(t *testing.T) {
	cfg := config.DefaultAppConfig()
	gateway := &fakeAdminGatewayController{}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")
	app.feishuRuntime.setup = &fakeFeishuSetupClient{
		describeResult: feishuAppIdentity{DisplayName: "Auto Named Bot"},
	}

	rec := performAdminRequest(t, app, http.MethodPost, "/api/setup/feishu/apps", `{"id":"main","appId":"cli_xxx","appSecret":"secret_xxx"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	var payload feishuAppResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if payload.App.Name != "Auto Named Bot" {
		t.Fatalf("expected auto-filled app name, got %#v", payload.App)
	}
}

func TestFeishuAppIDChangeResetsVerification(t *testing.T) {
	cfg := config.DefaultAppConfig()
	gateway := &fakeAdminGatewayController{}
	app, configPath := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/verify", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(verify): %v", err)
	}
	if loaded.Config.Feishu.Apps[0].VerifiedAt == nil {
		t.Fatalf("expected verifiedAt to be set before app id change, got %#v", loaded.Config.Feishu.Apps[0])
	}

	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"appId":"cli_new"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update appId status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var updateResp feishuAppResponse
	if err := json.NewDecoder(rec.Body).Decode(&updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResp.Mutation == nil || updateResp.Mutation.Kind != "identity_changed" || !updateResp.Mutation.RequiresNewChat {
		t.Fatalf("unexpected identity-change mutation: %#v", updateResp.Mutation)
	}
	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(reset): %v", err)
	}
	if loaded.Config.Feishu.Apps[0].VerifiedAt != nil {
		t.Fatalf("expected app id change to reset verification, got %#v", loaded.Config.Feishu.Apps[0])
	}
}

func TestFeishuAppSecretChangeReturnsCredentialsMutation(t *testing.T) {
	cfg := config.DefaultAppConfig()
	gateway := &fakeAdminGatewayController{}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"appSecret":"secret_new"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update secret status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var updateResp feishuAppResponse
	if err := json.NewDecoder(rec.Body).Decode(&updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResp.Mutation == nil || updateResp.Mutation.Kind != "credentials_changed" || !updateResp.Mutation.ReconnectRequested {
		t.Fatalf("unexpected credentials-change mutation: %#v", updateResp.Mutation)
	}
}

func TestFeishuCreateFailureSurfacesSavedButNotAppliedStateAndRetry(t *testing.T) {
	cfg := config.DefaultAppConfig()
	gateway := &fakeAdminGatewayController{
		upsertErrs: []error{errors.New("dial tcp 127.0.0.1:443: connect refused")},
	}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("create status = %d, want 500 body=%s", rec.Code, rec.Body.String())
	}
	var apiErr apiErrorPayload
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode api error: %v", err)
	}
	if apiErr.Error.Code != "gateway_apply_failed" || !apiErr.Error.Retryable {
		t.Fatalf("unexpected api error: %#v", apiErr.Error)
	}

	rec = performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/apps", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var apps feishuAppsResponse
	if err := json.NewDecoder(rec.Body).Decode(&apps); err != nil {
		t.Fatalf("decode apps: %v", err)
	}
	if len(apps.Apps) != 1 || apps.Apps[0].RuntimeApply == nil || !apps.Apps[0].RuntimeApply.Pending || apps.Apps[0].RuntimeApply.Action != feishuRuntimeApplyActionUpsert {
		t.Fatalf("expected pending upsert state after failed apply, got %#v", apps.Apps)
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/retry-apply", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("retry status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}

	rec = performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/apps", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list-after-retry status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	apps = feishuAppsResponse{}
	if err := json.NewDecoder(rec.Body).Decode(&apps); err != nil {
		t.Fatalf("decode apps after retry: %v", err)
	}
	if len(apps.Apps) != 1 || apps.Apps[0].RuntimeApply != nil {
		t.Fatalf("expected retry to clear pending runtime apply state, got %#v", apps.Apps)
	}
}

func TestFeishuDeleteFailureKeepsPendingRemovalVisibleUntilRetry(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main Bot",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	gateway := &fakeAdminGatewayController{
		statuses: []feishu.GatewayStatus{{
			GatewayID: "main",
			Name:      "Main Bot",
			State:     feishu.GatewayStateConnected,
		}},
		removeErrs: []error{errors.New("remove worker failed")},
	}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")

	rec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/feishu/apps/main", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete status = %d, want 500 body=%s", rec.Code, rec.Body.String())
	}

	rec = performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/apps", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var apps feishuAppsResponse
	if err := json.NewDecoder(rec.Body).Decode(&apps); err != nil {
		t.Fatalf("decode apps: %v", err)
	}
	if len(apps.Apps) != 1 {
		t.Fatalf("expected pending deleted app to remain visible, got %#v", apps.Apps)
	}
	if apps.Apps[0].Persisted || apps.Apps[0].RuntimeApply == nil || apps.Apps[0].RuntimeApply.Action != feishuRuntimeApplyActionRemove {
		t.Fatalf("expected pending removal state, got %#v", apps.Apps[0])
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/retry-apply", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("retry-delete status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}

	rec = performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/apps", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list-after-retry status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	apps = feishuAppsResponse{}
	if err := json.NewDecoder(rec.Body).Decode(&apps); err != nil {
		t.Fatalf("decode apps after retry: %v", err)
	}
	if len(apps.Apps) != 0 {
		t.Fatalf("expected retry delete to clear pending entry, got %#v", apps.Apps)
	}
}

func TestFeishuAppsListMarksEnvOverrideReadOnly(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Config Main",
		AppID:     "cli_config",
		AppSecret: "secret_config",
	}}
	services := defaultFeishuServices()
	services.FeishuGatewayID = "main"
	services.FeishuAppID = "cli_env"
	services.FeishuAppSecret = "secret_env"

	gateway := &fakeAdminGatewayController{
		statuses: []feishu.GatewayStatus{{
			GatewayID: "main",
			State:     feishu.GatewayStateConnected,
		}},
	}
	app, _ := newFeishuAdminTestApp(t, cfg, services, gateway, true, "main")

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/apps", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var payload feishuAppsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode apps: %v", err)
	}
	if len(payload.Apps) != 1 {
		t.Fatalf("expected one app, got %#v", payload.Apps)
	}
	appSummary := payload.Apps[0]
	if !appSummary.ReadOnly || !appSummary.RuntimeOverride {
		t.Fatalf("expected read-only runtime override, got %#v", appSummary)
	}
	if appSummary.AppID != "cli_env" {
		t.Fatalf("expected runtime app id, got %#v", appSummary)
	}
	if appSummary.Status == nil || appSummary.Status.State != feishu.GatewayStateConnected {
		t.Fatalf("expected connected status, got %#v", appSummary.Status)
	}

	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"name":"Should Fail"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("update status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "runtime_override_read_only") {
		t.Fatalf("unexpected read-only error body: %s", rec.Body.String())
	}
}

func newFeishuAdminTestApp(t *testing.T, cfg config.AppConfig, services config.ServicesConfig, gateway feishu.GatewayController, envOverrideActive bool, envOverrideGatewayID string) (*App, string) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		Paths: relayruntime.Paths{
			StateDir: t.TempDir(),
		},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath:           configPath,
		Services:             services,
		AdminListenHost:      services.RelayAPIHost,
		AdminListenPort:      services.RelayAPIPort,
		AdminURL:             "http://localhost:" + services.RelayAPIPort + "/admin/",
		SetupURL:             "http://localhost:" + services.RelayAPIPort + "/setup",
		EnvOverrideActive:    envOverrideActive,
		EnvOverrideGatewayID: envOverrideGatewayID,
	})
	return app, configPath
}

func defaultFeishuServices() config.ServicesConfig {
	return config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
}

func performAdminRequest(t *testing.T, app *App, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	return rec
}
