package daemon

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"
)

type feishuRegistrationRunner interface {
	Start(context.Context, feishuRegistrationOptions, feishuRegistrationCallbacks) feishuRegistrationRun
}

type feishuRegistrationRun interface {
	Cancel()
}

type feishuRegistrationOptions struct {
	Source     string
	Addons     *registration.AppAddons
	CreateOnly bool
	AppID      string
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

type sdkFeishuRegistrationRunner struct{}

func newLiveFeishuRegistrationRunner() feishuRegistrationRunner {
	return sdkFeishuRegistrationRunner{}
}

func (sdkFeishuRegistrationRunner) Start(ctx context.Context, options feishuRegistrationOptions, callbacks feishuRegistrationCallbacks) feishuRegistrationRun {
	runCtx, cancel := context.WithCancel(ctx)
	run := &sdkFeishuRegistrationRun{cancel: cancel}
	go run.start(runCtx, options, callbacks)
	return run
}

type sdkFeishuRegistrationRun struct {
	cancel context.CancelFunc
}

func (r *sdkFeishuRegistrationRun) Cancel() {
	if r != nil && r.cancel != nil {
		r.cancel()
	}
}

func (r *sdkFeishuRegistrationRun) start(ctx context.Context, options feishuRegistrationOptions, callbacks feishuRegistrationCallbacks) {
	result, err := registration.RegisterApp(ctx, &registration.Options{
		Source:     firstNonEmpty(strings.TrimSpace(options.Source), "codex-remote-feishu"),
		Addons:     options.Addons,
		CreateOnly: options.CreateOnly,
		AppID:      strings.TrimSpace(options.AppID),
		OnQRCode: func(info *registration.QRCodeInfo) {
			if callbacks.OnQRCode == nil || info == nil {
				return
			}
			expiresAt := time.Now().UTC().Add(time.Duration(info.ExpireIn) * time.Second)
			callbacks.OnQRCode(feishuRegistrationQRCode{
				URL:       strings.TrimSpace(info.URL),
				ExpiresAt: expiresAt,
				Interval:  5 * time.Second,
			})
		},
	})
	if err != nil {
		if callbacks.OnFailure != nil {
			callbacks.OnFailure(registrationFailureFromError(err))
		}
		return
	}
	if callbacks.OnComplete == nil || result == nil {
		return
	}
	installerID := ""
	if result.UserInfo != nil {
		installerID = strings.TrimSpace(result.UserInfo.OpenID)
	}
	callbacks.OnComplete(feishuRegistrationResult{
		AppID:       strings.TrimSpace(result.ClientID),
		AppSecret:   strings.TrimSpace(result.ClientSecret),
		InstallerID: installerID,
	})
}

func registrationFailureFromError(err error) feishuRegistrationFailure {
	if err == nil {
		return feishuRegistrationFailure{}
	}
	var accessDenied *registration.AccessDeniedError
	if errors.As(err, &accessDenied) {
		return feishuRegistrationFailure{
			Status:       feishuOnboardingStatusFailed,
			ErrorCode:    "access_denied",
			ErrorMessage: "扫码授权已取消，请重新开始。",
		}
	}
	var expired *registration.ExpiredError
	if errors.As(err, &expired) {
		return feishuRegistrationFailure{
			Status:       feishuOnboardingStatusExpired,
			ErrorCode:    "expired_token",
			ErrorMessage: "二维码已过期，请重新开始扫码。",
		}
	}
	var registerErr *registration.RegisterAppError
	if errors.As(err, &registerErr) {
		return feishuRegistrationFailure{
			Status:       feishuOnboardingStatusFailed,
			ErrorCode:    strings.TrimSpace(registerErr.Code),
			ErrorMessage: firstNonEmpty(strings.TrimSpace(registerErr.Description), "飞书返回了未识别的扫码结果。"),
		}
	}
	return feishuRegistrationFailure{
		Status:       feishuOnboardingStatusFailed,
		ErrorCode:    "feishu_onboarding_failed",
		ErrorMessage: err.Error(),
	}
}

func buildFeishuRegistrationAddons(manifest feishuapp.Manifest) *registration.AppAddons {
	preset := false
	addons := &registration.AppAddons{Preset: &preset}
	addons.Scopes.Tenant = sortedUniqueNonEmpty(scopeRequirementsForType(manifest.ScopeRequirements, "tenant"))
	addons.Scopes.User = sortedUniqueNonEmpty(scopeRequirementsForType(manifest.ScopeRequirements, "user"))
	addons.Events.Items.Tenant = sortedUniqueNonEmpty(eventRequirements(manifest.Events))
	addons.Callbacks.Items = sortedUniqueNonEmpty(callbackRequirements(manifest.Callbacks))
	return addons
}

func scopeRequirementsForType(requirements []feishuapp.ScopeRequirement, scopeType string) []string {
	values := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		if strings.TrimSpace(requirement.Scope) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(requirement.ScopeType), scopeType) {
			values = append(values, strings.TrimSpace(requirement.Scope))
		}
	}
	return values
}

func eventRequirements(requirements []feishuapp.EventRequirement) []string {
	values := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		values = append(values, strings.TrimSpace(requirement.Event))
	}
	return values
}

func callbackRequirements(requirements []feishuapp.CallbackRequirement) []string {
	values := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		values = append(values, strings.TrimSpace(requirement.Callback))
	}
	return values
}

func sortedUniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
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
	run := runner.Start(runCtx, feishuRegistrationOptions{
		Source:     "codex-remote-feishu",
		Addons:     buildFeishuRegistrationAddons(feishuapp.DefaultManifest()),
		CreateOnly: true,
	}, a.feishuRegistrationCallbacks(session.ID, cancel))
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
	now := time.Now().UTC()
	interval := info.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	session.Status = feishuOnboardingStatusPending
	session.VerificationURL = strings.TrimSpace(info.URL)
	session.QRCodeDataURL = qrCodeDataURL
	session.ExpiresAt = info.ExpiresAt.UTC()
	session.PollInterval = interval
	session.NextPollAt = now.Add(interval)
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
