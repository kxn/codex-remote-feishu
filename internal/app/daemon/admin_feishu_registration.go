package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const feishuRegistrationAccountsBaseURL = "https://accounts.feishu.cn"

type feishuRegistrationRunner interface {
	Start(context.Context, feishuRegistrationOptions, feishuRegistrationCallbacks) feishuRegistrationRun
}

type feishuRegistrationRun interface {
	Cancel()
}

type feishuRegistrationOptions struct {
}

type feishuRegistrationQRCode struct {
	URL       string
	ExpiresAt time.Time
	Interval  time.Duration
}

type feishuRegistrationResult struct {
	AppID       string
	AppSecret   string
	InstallerID string
}

type feishuRegistrationFailure struct {
	Status       string
	ErrorCode    string
	ErrorMessage string
}

type feishuRegistrationCallbacks struct {
	OnQRCode   func(feishuRegistrationQRCode)
	OnComplete func(feishuRegistrationResult)
	OnFailure  func(feishuRegistrationFailure)
}

const defaultFeishuRegistrationTimeout = 10 * time.Minute

type feishuRegistrationHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type legacyFeishuRegistrationRunner struct {
	httpClient      feishuRegistrationHTTPClient
	registrationURL string
	waitFn          func(context.Context, time.Duration) error
}

func newLiveFeishuRegistrationRunner() feishuRegistrationRunner {
	return &legacyFeishuRegistrationRunner{
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		registrationURL: feishuRegistrationAccountsBaseURL + "/oauth/v1/app/registration",
		waitFn:          waitForFeishuRegistration,
	}
}

func (r *legacyFeishuRegistrationRunner) Start(ctx context.Context, _ feishuRegistrationOptions, callbacks feishuRegistrationCallbacks) feishuRegistrationRun {
	runCtx, cancel := context.WithCancel(ctx)
	run := &legacyFeishuRegistrationRun{cancel: cancel}
	go func() {
		defer cancel()
		r.start(runCtx, callbacks)
	}()
	return run
}

type legacyFeishuRegistrationRun struct {
	cancel context.CancelFunc
}

func (r *legacyFeishuRegistrationRun) Cancel() {
	if r != nil && r.cancel != nil {
		r.cancel()
	}
}

type feishuRegistrationInitResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type feishuRegistrationBeginResponse struct {
	DeviceCode              string `json:"device_code"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpireIn                int    `json:"expire_in"`
	Error                   string `json:"error"`
	ErrorDescription        string `json:"error_description"`
}

type feishuRegistrationPollResponse struct {
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

type legacyFeishuRegistrationStart struct {
	deviceCode      string
	verificationURL string
	interval        time.Duration
	expiresAt       time.Time
}

type legacyFeishuRegistrationPoll struct {
	status       string
	appID        string
	appSecret    string
	installerID  string
	errorCode    string
	errorMessage string
	retryAfter   time.Duration
}

func (r *legacyFeishuRegistrationRunner) start(ctx context.Context, callbacks feishuRegistrationCallbacks) {
	started, err := r.startRegistration(ctx)
	if err != nil {
		r.emitFailure(ctx, callbacks, feishuRegistrationFailure{Status: feishuOnboardingStatusFailed, ErrorCode: "feishu_onboarding_failed", ErrorMessage: err.Error()})
		return
	}
	if ctx.Err() != nil {
		return
	}
	if callbacks.OnQRCode != nil {
		callbacks.OnQRCode(feishuRegistrationQRCode{
			URL:       started.verificationURL,
			ExpiresAt: started.expiresAt,
			Interval:  started.interval,
		})
	}

	interval := started.interval
	for {
		if !started.expiresAt.IsZero() && !time.Now().UTC().Before(started.expiresAt) {
			r.emitFailure(ctx, callbacks, feishuRegistrationFailure{
				Status:       feishuOnboardingStatusExpired,
				ErrorCode:    "expired_token",
				ErrorMessage: "二维码已过期，请重新开始扫码。",
			})
			return
		}
		if err := r.waitFor(ctx, interval); err != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		if !started.expiresAt.IsZero() && !time.Now().UTC().Before(started.expiresAt) {
			r.emitFailure(ctx, callbacks, feishuRegistrationFailure{
				Status:       feishuOnboardingStatusExpired,
				ErrorCode:    "expired_token",
				ErrorMessage: "二维码已过期，请重新开始扫码。",
			})
			return
		}

		poll, err := r.pollRegistration(ctx, started.deviceCode)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		if poll.retryAfter > 0 {
			interval = poll.retryAfter
		} else {
			interval = started.interval
		}
		switch poll.status {
		case feishuOnboardingStatusPending:
			continue
		case feishuOnboardingStatusReady:
			if callbacks.OnComplete != nil {
				callbacks.OnComplete(feishuRegistrationResult{
					AppID:       poll.appID,
					AppSecret:   poll.appSecret,
					InstallerID: poll.installerID,
				})
			}
			return
		case feishuOnboardingStatusExpired, feishuOnboardingStatusFailed:
			r.emitFailure(ctx, callbacks, feishuRegistrationFailure{
				Status:       poll.status,
				ErrorCode:    poll.errorCode,
				ErrorMessage: poll.errorMessage,
			})
			return
		default:
			r.emitFailure(ctx, callbacks, feishuRegistrationFailure{
				Status:       feishuOnboardingStatusFailed,
				ErrorCode:    "feishu_onboarding_failed",
				ErrorMessage: "飞书返回了未识别的扫码结果。",
			})
			return
		}
	}
}

func (r *legacyFeishuRegistrationRunner) startRegistration(ctx context.Context) (legacyFeishuRegistrationStart, error) {
	var initResp feishuRegistrationInitResponse
	if err := r.registrationCall(ctx, "init", nil, &initResp); err != nil {
		return legacyFeishuRegistrationStart{}, err
	}
	if initResp.Error != "" {
		return legacyFeishuRegistrationStart{}, registrationResponseError("init", initResp.Error, initResp.ErrorDescription)
	}

	var beginResp feishuRegistrationBeginResponse
	if err := r.registrationCall(ctx, "begin", map[string]string{
		"archetype":         "PersonalAgent",
		"auth_method":       "client_secret",
		"request_user_info": "open_id",
	}, &beginResp); err != nil {
		return legacyFeishuRegistrationStart{}, err
	}
	if beginResp.Error != "" {
		return legacyFeishuRegistrationStart{}, registrationResponseError("begin", beginResp.Error, beginResp.ErrorDescription)
	}
	if strings.TrimSpace(beginResp.DeviceCode) == "" || strings.TrimSpace(beginResp.VerificationURIComplete) == "" {
		return legacyFeishuRegistrationStart{}, errors.New("registration flow returned incomplete onboarding data")
	}

	interval := time.Duration(beginResp.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expiresIn := time.Duration(beginResp.ExpireIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = defaultFeishuRegistrationTimeout
	}
	return legacyFeishuRegistrationStart{
		deviceCode:      strings.TrimSpace(beginResp.DeviceCode),
		verificationURL: strings.TrimSpace(beginResp.VerificationURIComplete),
		interval:        interval,
		expiresAt:       time.Now().UTC().Add(expiresIn),
	}, nil
}

func (r *legacyFeishuRegistrationRunner) pollRegistration(ctx context.Context, deviceCode string) (legacyFeishuRegistrationPoll, error) {
	var pollResp feishuRegistrationPollResponse
	if err := r.registrationCall(ctx, "poll", map[string]string{"device_code": strings.TrimSpace(deviceCode)}, &pollResp); err != nil {
		return legacyFeishuRegistrationPoll{}, err
	}
	if strings.TrimSpace(pollResp.ClientID) != "" && strings.TrimSpace(pollResp.ClientSecret) != "" {
		return legacyFeishuRegistrationPoll{
			status:      feishuOnboardingStatusReady,
			appID:       strings.TrimSpace(pollResp.ClientID),
			appSecret:   strings.TrimSpace(pollResp.ClientSecret),
			installerID: firstNonEmpty(strings.TrimSpace(pollResp.OpenID), strings.TrimSpace(pollResp.UserOpenID), strings.TrimSpace(pollResp.UserInfo.OpenID)),
		}, nil
	}

	switch strings.TrimSpace(pollResp.Error) {
	case "", "authorization_pending":
		return legacyFeishuRegistrationPoll{status: feishuOnboardingStatusPending}, nil
	case "slow_down":
		return legacyFeishuRegistrationPoll{status: feishuOnboardingStatusPending, retryAfter: 5 * time.Second}, nil
	case "expired_token":
		return legacyFeishuRegistrationPoll{
			status:       feishuOnboardingStatusExpired,
			errorCode:    "expired_token",
			errorMessage: "二维码已过期，请重新开始扫码。",
		}, nil
	case "access_denied":
		return legacyFeishuRegistrationPoll{
			status:       feishuOnboardingStatusFailed,
			errorCode:    "access_denied",
			errorMessage: "扫码授权已取消，请重新开始。",
		}, nil
	default:
		if strings.TrimSpace(pollResp.Error) == "" {
			return legacyFeishuRegistrationPoll{status: feishuOnboardingStatusPending}, nil
		}
		return legacyFeishuRegistrationPoll{
			status:       feishuOnboardingStatusFailed,
			errorCode:    strings.TrimSpace(pollResp.Error),
			errorMessage: firstNonEmpty(strings.TrimSpace(pollResp.ErrorDescription), "飞书返回了未识别的扫码结果。"),
		}, nil
	}
}

func (r *legacyFeishuRegistrationRunner) registrationCall(ctx context.Context, action string, params map[string]string, out any) error {
	form := url.Values{"action": []string{strings.TrimSpace(action)}}
	for key, value := range params {
		form.Set(key, value)
	}
	registrationURL := strings.TrimSpace(r.registrationURL)
	if registrationURL == "" {
		registrationURL = feishuRegistrationAccountsBaseURL + "/oauth/v1/app/registration"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := r.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registration %s failed: status=%d", strings.TrimSpace(action), resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return err
	}
	return nil
}

func (r *legacyFeishuRegistrationRunner) waitFor(ctx context.Context, interval time.Duration) error {
	if r.waitFn != nil {
		return r.waitFn(ctx, interval)
	}
	return waitForFeishuRegistration(ctx, interval)
}

func (r *legacyFeishuRegistrationRunner) emitFailure(ctx context.Context, callbacks feishuRegistrationCallbacks, failure feishuRegistrationFailure) {
	if ctx.Err() == nil && callbacks.OnFailure != nil {
		callbacks.OnFailure(failure)
	}
}

func registrationResponseError(action, code, description string) error {
	return fmt.Errorf("registration %s returned %s: %s", strings.TrimSpace(action), strings.TrimSpace(code), strings.TrimSpace(description))
}

func waitForFeishuRegistration(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) createFeishuOnboardingSession(ctx context.Context) (feishuOnboardingSessionView, error) {
	a.cleanupFeishuOnboardingSessions(time.Now().UTC())
	sessionID, err := randomHex(12)
	if err != nil {
		return feishuOnboardingSessionView{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(defaultFeishuRegistrationTimeout)
	session := &feishuOnboardingSession{
		ID:        sessionID,
		Status:    feishuOnboardingStatusPending,
		ExpiresAt: expiresAt,
	}
	a.feishuRuntime.mu.Lock()
	if a.feishuRuntime.onboarding == nil {
		a.feishuRuntime.onboarding = map[string]*feishuOnboardingSession{}
	}
	a.feishuRuntime.onboarding[session.ID] = session
	a.feishuRuntime.mu.Unlock()

	runner := a.feishuRuntime.registration
	if runner == nil {
		runner = newLiveFeishuRegistrationRunner()
	}
	runCtx, cancel := context.WithTimeout(context.Background(), defaultFeishuRegistrationTimeout)
	run := runner.Start(runCtx, feishuRegistrationOptions{}, a.feishuRegistrationCallbacks(session.ID, cancel))
	run = feishuRegistrationRunWithCancel{
		run:    run,
		cancel: cancel,
	}
	a.feishuRuntime.mu.Lock()
	view := feishuOnboardingSessionView{}
	if stored := a.feishuRuntime.onboarding[session.ID]; stored != nil {
		stored.RegistrationRun = run
		if feishuOnboardingStatusIsTerminal(stored.Status) {
			cancelFeishuRegistrationRunLocked(stored)
		}
		view = feishuOnboardingSessionToView(stored)
	}
	a.feishuRuntime.mu.Unlock()
	return view, nil
}

func (a *App) feishuRegistrationCallbacks(sessionID string, cancel context.CancelFunc) feishuRegistrationCallbacks {
	return feishuRegistrationCallbacks{
		OnQRCode: func(info feishuRegistrationQRCode) {
			a.applyFeishuRegistrationQRCode(sessionID, info)
		},
		OnComplete: func(result feishuRegistrationResult) {
			if cancel != nil {
				cancel()
			}
			a.applyFeishuRegistrationResult(sessionID, result)
		},
		OnFailure: func(failure feishuRegistrationFailure) {
			if cancel != nil {
				cancel()
			}
			a.applyFeishuRegistrationFailure(sessionID, failure)
		},
	}
}

func (a *App) applyFeishuRegistrationQRCode(sessionID string, info feishuRegistrationQRCode) {
	qrCodeDataURL, err := qrCodeDataURL(info.URL)
	if err != nil {
		a.applyFeishuRegistrationFailure(sessionID, feishuRegistrationFailure{
			Status:       feishuOnboardingStatusFailed,
			ErrorCode:    "qr_code_render_failed",
			ErrorMessage: "二维码生成失败，请重新开始。",
		})
		return
	}
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	session := a.feishuRuntime.onboarding[strings.TrimSpace(sessionID)]
	if session == nil {
		return
	}
	interval := info.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	session.Status = feishuOnboardingStatusPending
	session.VerificationURL = strings.TrimSpace(info.URL)
	session.QRCodeDataURL = qrCodeDataURL
	session.ExpiresAt = info.ExpiresAt.UTC()
	session.PollInterval = interval
}

func (a *App) applyFeishuRegistrationResult(sessionID string, result feishuRegistrationResult) {
	displayName := a.suggestFeishuAppName(context.Background(), "", result.AppID, result.AppSecret, result.AppID)
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	session := a.feishuRuntime.onboarding[strings.TrimSpace(sessionID)]
	if session == nil {
		return
	}
	session.Status = feishuOnboardingStatusReady
	session.AppID = strings.TrimSpace(result.AppID)
	session.AppSecret = strings.TrimSpace(result.AppSecret)
	session.InstallerID = strings.TrimSpace(result.InstallerID)
	session.DisplayName = firstNonEmpty(displayName, session.AppID)
	session.ErrorCode = ""
	session.ErrorMessage = ""
	cancelFeishuRegistrationRunLocked(session)
}

func (a *App) applyFeishuRegistrationFailure(sessionID string, failure feishuRegistrationFailure) {
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	session := a.feishuRuntime.onboarding[strings.TrimSpace(sessionID)]
	if session == nil {
		return
	}
	status := strings.TrimSpace(failure.Status)
	if status == "" {
		status = feishuOnboardingStatusFailed
	}
	session.Status = status
	session.ErrorCode = strings.TrimSpace(failure.ErrorCode)
	session.ErrorMessage = strings.TrimSpace(failure.ErrorMessage)
	cancelFeishuRegistrationRunLocked(session)
}

func feishuOnboardingStatusIsTerminal(status string) bool {
	switch status {
	case feishuOnboardingStatusReady, feishuOnboardingStatusCompleted, feishuOnboardingStatusExpired, feishuOnboardingStatusFailed:
		return true
	default:
		return false
	}
}

func cancelFeishuRegistrationRunLocked(session *feishuOnboardingSession) {
	if session == nil || session.RegistrationRun == nil {
		return
	}
	run := session.RegistrationRun
	session.RegistrationRun = nil
	run.Cancel()
}

type feishuRegistrationRunWithCancel struct {
	run    feishuRegistrationRun
	cancel context.CancelFunc
}

func (r feishuRegistrationRunWithCancel) Cancel() {
	if r.cancel != nil {
		r.cancel()
	}
	if r.run != nil {
		r.run.Cancel()
	}
}
