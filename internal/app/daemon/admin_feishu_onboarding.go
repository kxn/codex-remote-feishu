package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/skip2/go-qrcode"
)

const (
	feishuOnboardingStatusPending   = "pending"
	feishuOnboardingStatusReady     = "ready"
	feishuOnboardingStatusCompleted = "completed"
	feishuOnboardingStatusExpired   = "expired"
	feishuOnboardingStatusFailed    = "failed"

	feishuRegistrationAccountsBaseURL = "https://accounts.feishu.cn"
	feishuRegistrationOpenBaseURL     = "https://open.feishu.cn"
)

type feishuSetupClient interface {
	StartRegistration(context.Context) (feishuRegistrationStartResult, error)
	PollRegistration(context.Context, string) (feishuRegistrationPollResult, error)
	DescribeApp(context.Context, string, string) (feishuAppIdentity, error)
}

type liveFeishuSetupClient struct {
	httpClient   *http.Client
	publicBroker *feishu.FeishuCallBroker
}

type feishuRegistrationStartResult struct {
	DeviceCode      string
	VerificationURL string
	Interval        time.Duration
	ExpiresAt       time.Time
}

type feishuRegistrationPollResult struct {
	Status       string
	AppID        string
	AppSecret    string
	InstallerID  string
	ErrorCode    string
	ErrorMessage string
	RetryAfter   time.Duration
}

type feishuAppIdentity struct {
	DisplayName string
}

type feishuOnboardingSession struct {
	ID                 string
	Status             string
	DeviceCode         string
	VerificationURL    string
	QRCodeDataURL      string
	ExpiresAt          time.Time
	PollInterval       time.Duration
	NextPollAt         time.Time
	LastPolledAt       time.Time
	AppID              string
	AppSecret          string
	InstallerID        string
	DisplayName        string
	ErrorCode          string
	ErrorMessage       string
	PersistedGatewayID string
	CompletedApp       *adminFeishuAppSummary
	CompletedMutation  *feishuAppMutationView
	CompletedResult    *feishu.VerifyResult
}

type feishuOnboardingSessionView struct {
	ID                  string     `json:"id"`
	Status              string     `json:"status"`
	VerificationURL     string     `json:"verificationUrl,omitempty"`
	QRCodeDataURL       string     `json:"qrCodeDataUrl,omitempty"`
	ExpiresAt           *time.Time `json:"expiresAt,omitempty"`
	PollIntervalSeconds int        `json:"pollIntervalSeconds,omitempty"`
	AppID               string     `json:"appId,omitempty"`
	DisplayName         string     `json:"displayName,omitempty"`
	ErrorCode           string     `json:"errorCode,omitempty"`
	ErrorMessage        string     `json:"errorMessage,omitempty"`
}

type feishuOnboardingSessionResponse struct {
	Session feishuOnboardingSessionView `json:"session"`
}

type feishuOnboardingCompleteResponse struct {
	App      adminFeishuAppSummary       `json:"app"`
	Mutation *feishuAppMutationView      `json:"mutation,omitempty"`
	Result   feishu.VerifyResult         `json:"result"`
	Session  feishuOnboardingSessionView `json:"session"`
	Guide    feishuOnboardingGuideView   `json:"guide,omitempty"`
}

type feishuOnboardingGuideView struct {
	AutoConfiguredSummary  string   `json:"autoConfiguredSummary,omitempty"`
	RemainingManualActions []string `json:"remainingManualActions,omitempty"`
	RecommendedNextStep    string   `json:"recommendedNextStep,omitempty"`
}

type registrationInitResponse struct {
	SupportedAuthMethods []string `json:"supported_auth_methods"`
	Error                string   `json:"error"`
	ErrorDescription     string   `json:"error_description"`
}

type registrationBeginResponse struct {
	DeviceCode              string `json:"device_code"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpireIn                int    `json:"expire_in"`
	Error                   string `json:"error"`
	ErrorDescription        string `json:"error_description"`
}

type registrationPollResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	OpenID       string `json:"open_id"`
	UserOpenID   string `json:"user_open_id"`
	UserInfo     struct {
		OpenID string `json:"open_id"`
	} `json:"user_info"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type tenantAccessTokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
}

type botInfoResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Bot  struct {
		AppName string `json:"app_name"`
		OpenID  string `json:"open_id"`
	} `json:"bot"`
}

func newLiveFeishuSetupClient() feishuSetupClient {
	httpClient := &http.Client{Timeout: 15 * time.Second}
	return &liveFeishuSetupClient{
		httpClient:   httpClient,
		publicBroker: feishu.NewFeishuCallBrokerWithHTTPClient("feishu-onboarding-registration", nil, httpClient),
	}
}

func (c *liveFeishuSetupClient) StartRegistration(ctx context.Context) (feishuRegistrationStartResult, error) {
	var initResp registrationInitResponse
	if err := c.registrationCall(ctx, "init", nil, &initResp); err != nil {
		return feishuRegistrationStartResult{}, err
	}
	if initResp.Error != "" {
		return feishuRegistrationStartResult{}, fmt.Errorf("%s: %s", initResp.Error, initResp.ErrorDescription)
	}

	var beginResp registrationBeginResponse
	if err := c.registrationCall(ctx, "begin", map[string]string{
		"archetype":         "PersonalAgent",
		"auth_method":       "client_secret",
		"request_user_info": "open_id",
	}, &beginResp); err != nil {
		return feishuRegistrationStartResult{}, err
	}
	if beginResp.Error != "" {
		return feishuRegistrationStartResult{}, fmt.Errorf("%s: %s", beginResp.Error, beginResp.ErrorDescription)
	}
	if strings.TrimSpace(beginResp.DeviceCode) == "" || strings.TrimSpace(beginResp.VerificationURIComplete) == "" {
		return feishuRegistrationStartResult{}, errors.New("registration flow returned incomplete onboarding data")
	}

	interval := time.Duration(beginResp.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expireIn := time.Duration(beginResp.ExpireIn) * time.Second
	if expireIn <= 0 {
		expireIn = 10 * time.Minute
	}
	now := time.Now().UTC()
	return feishuRegistrationStartResult{
		DeviceCode:      strings.TrimSpace(beginResp.DeviceCode),
		VerificationURL: strings.TrimSpace(beginResp.VerificationURIComplete),
		Interval:        interval,
		ExpiresAt:       now.Add(expireIn),
	}, nil
}

func (c *liveFeishuSetupClient) PollRegistration(ctx context.Context, deviceCode string) (feishuRegistrationPollResult, error) {
	var pollResp registrationPollResponse
	if err := c.registrationCall(ctx, "poll", map[string]string{"device_code": strings.TrimSpace(deviceCode)}, &pollResp); err != nil {
		return feishuRegistrationPollResult{}, err
	}
	if strings.TrimSpace(pollResp.ClientID) != "" && strings.TrimSpace(pollResp.ClientSecret) != "" {
		installerID := firstNonEmpty(
			strings.TrimSpace(pollResp.OpenID),
			strings.TrimSpace(pollResp.UserOpenID),
			strings.TrimSpace(pollResp.UserInfo.OpenID),
		)
		return feishuRegistrationPollResult{
			Status:      feishuOnboardingStatusReady,
			AppID:       strings.TrimSpace(pollResp.ClientID),
			AppSecret:   strings.TrimSpace(pollResp.ClientSecret),
			InstallerID: strings.TrimSpace(installerID),
		}, nil
	}

	switch strings.TrimSpace(pollResp.Error) {
	case "", "authorization_pending":
		return feishuRegistrationPollResult{Status: feishuOnboardingStatusPending}, nil
	case "slow_down":
		return feishuRegistrationPollResult{Status: feishuOnboardingStatusPending, RetryAfter: 5 * time.Second}, nil
	case "expired_token":
		return feishuRegistrationPollResult{
			Status:       feishuOnboardingStatusExpired,
			ErrorCode:    "expired_token",
			ErrorMessage: "二维码已过期，请重新开始扫码。",
		}, nil
	case "access_denied":
		return feishuRegistrationPollResult{
			Status:       feishuOnboardingStatusFailed,
			ErrorCode:    "access_denied",
			ErrorMessage: "扫码授权已取消，请重新开始。",
		}, nil
	default:
		if strings.TrimSpace(pollResp.Error) != "" {
			return feishuRegistrationPollResult{
				Status:       feishuOnboardingStatusFailed,
				ErrorCode:    strings.TrimSpace(pollResp.Error),
				ErrorMessage: firstNonEmpty(strings.TrimSpace(pollResp.ErrorDescription), "飞书返回了未识别的扫码结果。"),
			}, nil
		}
		return feishuRegistrationPollResult{Status: feishuOnboardingStatusPending}, nil
	}
}

func (c *liveFeishuSetupClient) DescribeApp(ctx context.Context, appID, appSecret string) (feishuAppIdentity, error) {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if appID == "" || appSecret == "" {
		return feishuAppIdentity{}, errors.New("missing app credentials")
	}

	payload, err := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})
	if err != nil {
		return feishuAppIdentity{}, err
	}
	var tokenResp tenantAccessTokenResponse
	appBroker := feishu.NewFeishuCallBrokerWithHTTPClient("feishu-onboarding-"+appID, nil, c.httpClient)
	_, err = feishu.DoHTTP(ctx, appBroker, feishu.CallSpec{
		GatewayID:  "feishu-onboarding-" + appID,
		API:        "auth.v3.tenant_access_token.internal",
		Class:      feishu.CallClassMetaHTTP,
		Priority:   feishu.CallPriorityInteractive,
		Retry:      feishu.RetryOff,
		Permission: feishu.PermissionFailFast,
	}, func(callCtx context.Context, httpClient *http.Client) (struct{}, error) {
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, feishuRegistrationOpenBaseURL+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(payload))
		if err != nil {
			return struct{}{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			return struct{}{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return struct{}{}, fmt.Errorf("tenant access token request failed: status=%d", resp.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokenResp); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
	if err != nil {
		return feishuAppIdentity{}, err
	}
	if tokenResp.Code != 0 || strings.TrimSpace(tokenResp.TenantAccessToken) == "" {
		return feishuAppIdentity{}, fmt.Errorf("tenant access token failed: code=%d msg=%s", tokenResp.Code, tokenResp.Msg)
	}

	var botResp botInfoResponse
	_, err = feishu.DoHTTP(ctx, appBroker, feishu.CallSpec{
		GatewayID:  "feishu-onboarding-" + appID,
		API:        "bot.v3.info",
		Class:      feishu.CallClassMetaHTTP,
		Priority:   feishu.CallPriorityInteractive,
		Retry:      feishu.RetrySafe,
		Permission: feishu.PermissionFailFast,
	}, func(callCtx context.Context, httpClient *http.Client) (struct{}, error) {
		infoReq, err := http.NewRequestWithContext(callCtx, http.MethodGet, feishuRegistrationOpenBaseURL+"/open-apis/bot/v3/info", nil)
		if err != nil {
			return struct{}{}, err
		}
		infoReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(tokenResp.TenantAccessToken))
		infoResp, err := httpClient.Do(infoReq)
		if err != nil {
			return struct{}{}, err
		}
		defer infoResp.Body.Close()
		if infoResp.StatusCode != http.StatusOK {
			return struct{}{}, fmt.Errorf("bot info request failed: status=%d", infoResp.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(infoResp.Body, 1<<20)).Decode(&botResp); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
	if err != nil {
		return feishuAppIdentity{}, err
	}
	if botResp.Code != 0 {
		return feishuAppIdentity{}, fmt.Errorf("bot info failed: code=%d msg=%s", botResp.Code, botResp.Msg)
	}
	return feishuAppIdentity{
		DisplayName: strings.TrimSpace(botResp.Bot.AppName),
	}, nil
}

func (c *liveFeishuSetupClient) registrationCall(ctx context.Context, action string, params map[string]string, out any) error {
	form := url.Values{}
	form.Set("action", action)
	for key, value := range params {
		form.Set(key, value)
	}
	broker := c.publicBroker
	if broker == nil {
		broker = feishu.NewFeishuCallBrokerWithHTTPClient("feishu-onboarding-registration", nil, c.httpClient)
	}
	_, err := feishu.DoHTTP(ctx, broker, feishu.CallSpec{
		GatewayID:  "feishu-onboarding-registration",
		API:        "oauth.v1.app.registration." + strings.TrimSpace(action),
		Class:      feishu.CallClassMetaHTTP,
		Priority:   feishu.CallPriorityInteractive,
		Retry:      feishu.RetryOff,
		Permission: feishu.PermissionFailFast,
	}, func(callCtx context.Context, httpClient *http.Client) (struct{}, error) {
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, feishuRegistrationAccountsBaseURL+"/oauth/v1/app/registration", strings.NewReader(form.Encode()))
		if err != nil {
			return struct{}{}, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := httpClient.Do(req)
		if err != nil {
			return struct{}{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return struct{}{}, fmt.Errorf("registration %s failed: status=%d", strings.TrimSpace(action), resp.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
	return err
}

func (a *App) snapshotFeishuOnboardingSession(sessionID string) (feishuOnboardingSessionView, bool) {
	a.feishuRuntime.mu.RLock()
	defer a.feishuRuntime.mu.RUnlock()
	if a.feishuRuntime.onboarding == nil {
		return feishuOnboardingSessionView{}, false
	}
	session, ok := a.feishuRuntime.onboarding[strings.TrimSpace(sessionID)]
	if !ok || session == nil {
		return feishuOnboardingSessionView{}, false
	}
	return feishuOnboardingSessionToView(session), true
}

func (a *App) cleanupFeishuOnboardingSessions(now time.Time) {
	now = now.UTC()
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	if len(a.feishuRuntime.onboarding) == 0 {
		return
	}
	for sessionID, session := range a.feishuRuntime.onboarding {
		if session == nil {
			delete(a.feishuRuntime.onboarding, sessionID)
			continue
		}
		if session.Status == feishuOnboardingStatusCompleted {
			continue
		}
		if !session.ExpiresAt.IsZero() && now.After(session.ExpiresAt.Add(15*time.Minute)) {
			delete(a.feishuRuntime.onboarding, sessionID)
		}
	}
}

func (a *App) createFeishuOnboardingSession(ctx context.Context) (feishuOnboardingSessionView, error) {
	a.cleanupFeishuOnboardingSessions(time.Now().UTC())
	result, err := a.feishuRuntime.setup.StartRegistration(ctx)
	if err != nil {
		return feishuOnboardingSessionView{}, err
	}
	sessionID, err := randomHex(12)
	if err != nil {
		return feishuOnboardingSessionView{}, err
	}
	qrCodeDataURL, err := qrCodeDataURL(result.VerificationURL)
	if err != nil {
		return feishuOnboardingSessionView{}, err
	}
	session := &feishuOnboardingSession{
		ID:              sessionID,
		Status:          feishuOnboardingStatusPending,
		DeviceCode:      result.DeviceCode,
		VerificationURL: result.VerificationURL,
		QRCodeDataURL:   qrCodeDataURL,
		ExpiresAt:       result.ExpiresAt.UTC(),
		PollInterval:    result.Interval,
		NextPollAt:      time.Now().UTC().Add(result.Interval),
	}
	a.feishuRuntime.mu.Lock()
	if a.feishuRuntime.onboarding == nil {
		a.feishuRuntime.onboarding = map[string]*feishuOnboardingSession{}
	}
	a.feishuRuntime.onboarding[session.ID] = session
	a.feishuRuntime.mu.Unlock()
	log.Printf(
		"feishu onboarding created: session=%s expires_at=%s poll_interval=%s",
		session.ID,
		session.ExpiresAt.Format(time.RFC3339),
		session.PollInterval,
	)
	return feishuOnboardingSessionToView(session), nil
}

func (a *App) refreshFeishuOnboardingSession(ctx context.Context, sessionID string) (feishuOnboardingSessionView, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	a.feishuRuntime.mu.Lock()
	session, ok := a.feishuRuntime.onboarding[sessionID]
	if !ok || session == nil {
		a.feishuRuntime.mu.Unlock()
		return feishuOnboardingSessionView{}, false, nil
	}

	now := time.Now().UTC()
	if session.Status == feishuOnboardingStatusPending && !session.ExpiresAt.IsZero() && now.After(session.ExpiresAt) {
		session.Status = feishuOnboardingStatusExpired
		session.ErrorCode = "expired_token"
		session.ErrorMessage = "二维码已过期，请重新开始扫码。"
	}
	shouldPoll := session.Status == feishuOnboardingStatusPending && (session.NextPollAt.IsZero() || !now.Before(session.NextPollAt))
	deviceCode := session.DeviceCode
	pollInterval := session.PollInterval
	a.feishuRuntime.mu.Unlock()

	if !shouldPoll {
		view, ok := a.snapshotFeishuOnboardingSession(sessionID)
		return view, ok, nil
	}

	pollCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	pollResult, err := a.feishuRuntime.setup.PollRegistration(pollCtx, deviceCode)
	if err != nil {
		a.feishuRuntime.mu.Lock()
		session, ok = a.feishuRuntime.onboarding[sessionID]
		if ok && session != nil {
			now := time.Now().UTC()
			session.Status = feishuOnboardingStatusPending
			session.LastPolledAt = now
			if pollInterval <= 0 {
				pollInterval = 5 * time.Second
			}
			session.NextPollAt = now.Add(pollInterval)
		}
		a.feishuRuntime.mu.Unlock()
		log.Printf("feishu onboarding poll transient error: session=%s err=%v", sessionID, err)
		view, ok := a.snapshotFeishuOnboardingSession(sessionID)
		return view, ok, nil
	}

	displayName := ""
	if pollResult.Status == feishuOnboardingStatusReady {
		displayName = a.suggestFeishuAppName(ctx, "", pollResult.AppID, pollResult.AppSecret, pollResult.AppID)
	}

	a.feishuRuntime.mu.Lock()
	session, ok = a.feishuRuntime.onboarding[sessionID]
	if !ok || session == nil {
		a.feishuRuntime.mu.Unlock()
		return feishuOnboardingSessionView{}, false, nil
	}
	now = time.Now().UTC()
	session.LastPolledAt = now
	if pollResult.RetryAfter > 0 {
		session.PollInterval += pollResult.RetryAfter
		if session.PollInterval <= 0 {
			session.PollInterval = 5 * time.Second
		}
		pollInterval = session.PollInterval
	}
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	session.NextPollAt = now.Add(pollInterval)

	switch pollResult.Status {
	case feishuOnboardingStatusReady:
		session.Status = feishuOnboardingStatusReady
		session.AppID = strings.TrimSpace(pollResult.AppID)
		session.AppSecret = strings.TrimSpace(pollResult.AppSecret)
		session.InstallerID = strings.TrimSpace(pollResult.InstallerID)
		session.DisplayName = firstNonEmpty(displayName, session.AppID)
		session.ErrorCode = ""
		session.ErrorMessage = ""
		log.Printf(
			"feishu onboarding ready: session=%s app_id=%s installer_id_present=%t",
			sessionID,
			session.AppID,
			strings.TrimSpace(session.InstallerID) != "",
		)
	case feishuOnboardingStatusExpired:
		session.Status = feishuOnboardingStatusExpired
		session.ErrorCode = pollResult.ErrorCode
		session.ErrorMessage = pollResult.ErrorMessage
		log.Printf("feishu onboarding expired: session=%s code=%s", sessionID, session.ErrorCode)
	case feishuOnboardingStatusFailed:
		session.Status = feishuOnboardingStatusFailed
		session.ErrorCode = pollResult.ErrorCode
		session.ErrorMessage = pollResult.ErrorMessage
		log.Printf(
			"feishu onboarding failed: session=%s code=%s message=%s",
			sessionID,
			session.ErrorCode,
			session.ErrorMessage,
		)
	default:
		session.Status = feishuOnboardingStatusPending
	}
	view := feishuOnboardingSessionToView(session)
	a.feishuRuntime.mu.Unlock()
	return view, true, nil
}

func (a *App) suggestFeishuAppName(ctx context.Context, requestedName, appID, appSecret, fallback string) string {
	if strings.TrimSpace(requestedName) != "" {
		return strings.TrimSpace(requestedName)
	}
	identity, err := a.resolveFeishuAppIdentity(ctx, appID, appSecret)
	if err == nil && strings.TrimSpace(identity.DisplayName) != "" {
		return strings.TrimSpace(identity.DisplayName)
	}
	return firstNonEmpty(strings.TrimSpace(appID), strings.TrimSpace(fallback))
}

func (a *App) resolveFeishuAppIdentity(ctx context.Context, appID, appSecret string) (feishuAppIdentity, error) {
	if a.feishuRuntime.setup == nil {
		return feishuAppIdentity{}, errors.New("feishu setup client unavailable")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return a.feishuRuntime.setup.DescribeApp(timeoutCtx, appID, appSecret)
}

func feishuOnboardingSessionToView(session *feishuOnboardingSession) feishuOnboardingSessionView {
	if session == nil {
		return feishuOnboardingSessionView{}
	}
	view := feishuOnboardingSessionView{
		ID:              session.ID,
		Status:          session.Status,
		VerificationURL: session.VerificationURL,
		QRCodeDataURL:   session.QRCodeDataURL,
		AppID:           session.AppID,
		DisplayName:     session.DisplayName,
		ErrorCode:       session.ErrorCode,
		ErrorMessage:    session.ErrorMessage,
	}
	if !session.ExpiresAt.IsZero() {
		expiresAt := session.ExpiresAt
		view.ExpiresAt = &expiresAt
	}
	if session.PollInterval > 0 {
		view.PollIntervalSeconds = int(session.PollInterval / time.Second)
	}
	return view
}

func buildFeishuOnboardingGuide() feishuOnboardingGuideView {
	return feishuOnboardingGuideView{
		AutoConfiguredSummary: "扫码创建已经完成，大部分基础配置已自动处理。",
		RemainingManualActions: []string{
			"如果需要把 Markdown 预览上传到飞书云盘，还需要额外申请 `drive:drive` 权限。",
			"这个权限通常需要管理员审批；如果暂时不需要 Markdown 预览，可以先直接继续。",
		},
		RecommendedNextStep: "runtimeRequirements",
	}
}

func qrCodeDataURL(value string) (string, error) {
	png, err := qrcode.Encode(strings.TrimSpace(value), qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func randomHex(size int) (string, error) {
	if size <= 0 {
		size = 12
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (a *App) handleFeishuOnboardingSessionCreate(w http.ResponseWriter, r *http.Request) {
	sessionCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	session, err := a.createFeishuOnboardingSession(sessionCtx)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, apiError{
			Code:    "feishu_onboarding_unavailable",
			Message: "failed to start feishu onboarding session",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusCreated, feishuOnboardingSessionResponse{Session: session})
}

func (a *App) handleFeishuOnboardingSessionGet(w http.ResponseWriter, r *http.Request) {
	session, ok, err := a.refreshFeishuOnboardingSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, apiError{
			Code:    "feishu_onboarding_unavailable",
			Message: "failed to refresh feishu onboarding session",
			Details: err.Error(),
		})
		return
	}
	if !ok {
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_onboarding_not_found",
			Message: "feishu onboarding session not found",
			Details: strings.TrimSpace(r.PathValue("id")),
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuOnboardingSessionResponse{Session: session})
}

func (a *App) handleFeishuOnboardingSessionComplete(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("id"))
	sessionView, ok, err := a.refreshFeishuOnboardingSession(r.Context(), sessionID)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, apiError{
			Code:    "feishu_onboarding_unavailable",
			Message: "failed to refresh feishu onboarding session",
			Details: err.Error(),
		})
		return
	}
	if !ok {
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_onboarding_not_found",
			Message: "feishu onboarding session not found",
			Details: sessionID,
		})
		return
	}

	a.feishuRuntime.mu.RLock()
	session, ok := a.feishuRuntime.onboarding[sessionID]
	if !ok || session == nil {
		a.feishuRuntime.mu.RUnlock()
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_onboarding_not_found",
			Message: "feishu onboarding session not found",
			Details: sessionID,
		})
		return
	}
	if session.Status == feishuOnboardingStatusCompleted && session.CompletedApp != nil && session.CompletedResult != nil {
		response := feishuOnboardingCompleteResponse{
			App:      *session.CompletedApp,
			Mutation: session.CompletedMutation,
			Result:   *session.CompletedResult,
			Session:  feishuOnboardingSessionToView(session),
			Guide:    buildFeishuOnboardingGuide(),
		}
		a.feishuRuntime.mu.RUnlock()
		writeJSON(w, http.StatusOK, response)
		return
	}
	sessionState := *session
	a.feishuRuntime.mu.RUnlock()

	switch sessionView.Status {
	case feishuOnboardingStatusPending:
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "feishu_onboarding_pending",
			Message: "feishu onboarding is still waiting for scan authorization",
			Details: "scan the QR code and wait for credentials to be issued",
		})
		return
	case feishuOnboardingStatusExpired:
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    firstNonEmpty(sessionView.ErrorCode, "expired_token"),
			Message: "the current QR code session has expired",
			Details: sessionView.ErrorMessage,
		})
		return
	case feishuOnboardingStatusFailed:
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    firstNonEmpty(sessionView.ErrorCode, "feishu_onboarding_failed"),
			Message: "the current QR onboarding session cannot be completed",
			Details: sessionView.ErrorMessage,
		})
		return
	case feishuOnboardingStatusReady:
	default:
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "feishu_onboarding_not_ready",
			Message: "the current QR onboarding session is not ready yet",
			Details: sessionView.Status,
		})
		return
	}

	if strings.TrimSpace(sessionState.AppID) == "" || strings.TrimSpace(sessionState.AppSecret) == "" {
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "feishu_onboarding_missing_credentials",
			Message: "the current QR onboarding session has not received credentials yet",
		})
		return
	}

	mutation := buildCreatedFeishuAppMutation()
	gatewayID := strings.TrimSpace(sessionState.PersistedGatewayID)
	var summary adminFeishuAppSummary

	if gatewayID == "" {
		created, nextGatewayID, createErr := a.createFeishuOnboardedApp(sessionState.DisplayName, sessionState.AppID, sessionState.AppSecret)
		if createErr != nil {
			if nextGatewayID != "" {
				a.markOnboardingSessionPersistedGateway(sessionID, nextGatewayID)
				a.writeFeishuRuntimeApplyError(w, nextGatewayID, created, feishuRuntimeApplyActionUpsert, "feishu app saved but runtime apply failed", createErr)
				return
			}
			writeAPIError(w, http.StatusInternalServerError, apiError{
				Code:    "config_write_failed",
				Message: "failed to save feishu app from onboarding session",
				Details: createErr.Error(),
			})
			return
		}
		gatewayID = nextGatewayID
		summary = created
		a.markOnboardingSessionPersistedGateway(sessionID, gatewayID)
	} else {
		loaded, err := a.loadAdminConfig()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, apiError{
				Code:    "config_unavailable",
				Message: "failed to load config for onboarding completion",
				Details: err.Error(),
			})
			return
		}
		reloadedSummary, ok, summaryErr := a.adminFeishuAppSummary(loaded, gatewayID)
		if summaryErr != nil || !ok {
			writeAPIError(w, http.StatusInternalServerError, apiError{
				Code:    "feishu_app_unavailable",
				Message: "failed to load feishu app created from onboarding session",
				Details: gatewayID,
			})
			return
		}
		summary = reloadedSummary
		if err := a.applyRuntimeFeishuConfig(loaded.Config, gatewayID); err != nil {
			a.writeFeishuRuntimeApplyError(w, gatewayID, summary, feishuRuntimeApplyActionUpsert, "failed to re-apply feishu runtime for onboarding session", err)
			return
		}
	}

	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config after onboarding completion",
			Details: err.Error(),
		})
		return
	}
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_unavailable",
			Message: "feishu app created from onboarding session is not available at runtime",
			Details: gatewayID,
		})
		return
	}
	controller, err := a.gatewayController()
	if err != nil {
		writeAPIError(w, http.StatusNotImplemented, apiError{
			Code:    "gateway_controller_unavailable",
			Message: "current gateway does not support runtime feishu management",
			Details: err.Error(),
		})
		return
	}
	verifyCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, verifyErr := controller.Verify(verifyCtx, runtimeCfg)
	if verifyErr == nil {
		if err := a.markFeishuAppOnboardingCompleted(loaded.Path, gatewayID, time.Now().UTC()); err != nil {
			writeAPIError(w, http.StatusInternalServerError, apiError{
				Code:    "config_write_failed",
				Message: "feishu app verified but failed to persist verification time",
				Details: err.Error(),
			})
			return
		}
		loaded, err = a.loadAdminConfig()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, apiError{
				Code:    "config_unavailable",
				Message: "failed to reload config after verification",
				Details: err.Error(),
			})
			return
		}
	}

	summary, ok, err = a.adminFeishuAppSummary(loaded, gatewayID)
	if err != nil || !ok {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_unavailable",
			Message: "failed to load feishu app after onboarding verification",
			Details: gatewayID,
		})
		return
	}

	response := feishuOnboardingCompleteResponse{
		App:      summary,
		Mutation: mutation,
		Result:   result,
		Session:  sessionView,
		Guide:    buildFeishuOnboardingGuide(),
	}
	if verifyErr != nil {
		log.Printf("feishu onboarding verify failed: session=%s gateway=%s err=%v", sessionID, gatewayID, verifyErr)
		writeJSON(w, http.StatusBadGateway, response)
		return
	}

	if strings.TrimSpace(sessionState.InstallerID) != "" {
		a.bindFeishuAppWebTestRecipient(gatewayID, sessionState.InstallerID)
	}
	response.Session = a.markOnboardingSessionCompleted(sessionID, gatewayID, summary, mutation, result)
	a.maybeSendFeishuAppVerifySuccessNotices(r.Context(), gatewayID, strings.HasPrefix(r.URL.Path, "/api/setup/"))
	log.Printf("feishu onboarding completed: session=%s gateway=%s app_id=%s", sessionID, gatewayID, summary.AppID)
	writeJSON(w, http.StatusOK, response)
}

func (a *App) createFeishuOnboardedApp(name, appID, appSecret string) (adminFeishuAppSummary, string, error) {
	a.adminConfigMu.Lock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		a.adminConfigMu.Unlock()
		return adminFeishuAppSummary{}, "", err
	}

	admin := a.snapshotAdminRuntime()
	req := feishuAppWriteRequest{
		Name:  daemonStringPtr(name),
		AppID: daemonStringPtr(appID),
	}
	gatewayID := nextGatewayID(loaded.Config.Feishu.Apps, admin, req)
	app := config.FeishuAppConfig{
		ID:        gatewayID,
		Name:      firstNonEmpty(strings.TrimSpace(name), strings.TrimSpace(appID), gatewayID),
		AppID:     strings.TrimSpace(appID),
		AppSecret: strings.TrimSpace(appSecret),
		Enabled:   daemonBoolPtr(true),
	}

	updated := loaded.Config
	updated.Feishu.Apps = append(updated.Feishu.Apps, app)
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		a.adminConfigMu.Unlock()
		return adminFeishuAppSummary{}, "", err
	}
	a.adminConfigMu.Unlock()

	summary, _, summaryErr := a.adminFeishuAppSummary(config.LoadedAppConfig{Path: loaded.Path, Config: updated}, gatewayID)
	if summaryErr != nil {
		summary = adminFeishuAppSummary{
			ID:        gatewayID,
			Name:      firstNonEmpty(strings.TrimSpace(app.Name), gatewayID),
			AppID:     strings.TrimSpace(app.AppID),
			HasSecret: strings.TrimSpace(app.AppSecret) != "",
			Enabled:   true,
			Persisted: true,
		}
	}
	if err := a.applyRuntimeFeishuConfig(updated, gatewayID); err != nil {
		return summary, gatewayID, err
	}
	return summary, gatewayID, nil
}

func (a *App) markOnboardingSessionPersistedGateway(sessionID, gatewayID string) {
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	session, ok := a.feishuRuntime.onboarding[strings.TrimSpace(sessionID)]
	if !ok || session == nil {
		return
	}
	session.PersistedGatewayID = strings.TrimSpace(gatewayID)
}

func (a *App) markOnboardingSessionCompleted(sessionID, gatewayID string, app adminFeishuAppSummary, mutation *feishuAppMutationView, result feishu.VerifyResult) feishuOnboardingSessionView {
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	session, ok := a.feishuRuntime.onboarding[strings.TrimSpace(sessionID)]
	if !ok || session == nil {
		return feishuOnboardingSessionView{}
	}
	appCopy := app
	resultCopy := result
	session.Status = feishuOnboardingStatusCompleted
	session.PersistedGatewayID = strings.TrimSpace(gatewayID)
	session.CompletedApp = &appCopy
	session.CompletedMutation = mutation
	session.CompletedResult = &resultCopy
	return feishuOnboardingSessionToView(session)
}

func daemonStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
