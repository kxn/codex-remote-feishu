package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
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
)

var errMissingFeishuAppCredentials = errors.New("missing app credentials")

type feishuAppIdentity struct {
	DisplayName string
}

type feishuOnboardingSession struct {
	ID                 string
	Status             string
	RegistrationRun    feishuRegistrationRun
	VerificationURL    string
	QRCodeDataURL      string
	ExpiresAt          time.Time
	PollInterval       time.Duration
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
			cancelFeishuRegistrationRunLocked(session)
			delete(a.feishuRuntime.onboarding, sessionID)
		}
	}
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
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return feishuSetupFacade.DescribeApp(timeoutCtx, appID, appSecret)
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
		AutoConfiguredSummary: "扫码创建已经完成，接下来会继续确认应用配置状态。",
		RemainingManualActions: []string{
			"如页面提示需要发布或审核，请按配置确认步骤处理。",
			"如果暂时无法确认长连接在线，可以稍后刷新或检查本机服务状态。",
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
		if applySummary, err := a.applyPersistedFeishuRuntime(loaded, gatewayID, config.FeishuAppConfig{}); err != nil {
			a.writeFeishuRuntimeApplyError(w, gatewayID, applySummary, feishuRuntimeApplyActionUpsert, "failed to re-apply feishu runtime for onboarding session", err)
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
	result, verifyErr, runtimeAvailable, err := a.verifyFeishuRuntimeConfig(r.Context(), loaded, gatewayID)
	if !runtimeAvailable {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_unavailable",
			Message: "feishu app created from onboarding session is not available at runtime",
			Details: gatewayID,
		})
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusNotImplemented, apiError{
			Code:    "gateway_controller_unavailable",
			Message: "current gateway does not support runtime feishu management",
			Details: err.Error(),
		})
		return
	}
	loaded, summary, ok, err = a.finalizePersistedFeishuVerification(
		loaded,
		gatewayID,
		verifyErr,
		feishuVerificationPersistOnboardingComplete,
	)
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

	response.Session = a.markOnboardingSessionCompleted(sessionID, gatewayID, summary, mutation, result)
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

	persisted := config.LoadedAppConfig{Path: loaded.Path, Config: updated}
	summary := a.loadPersistedFeishuAppSummaryOrFallback(persisted, gatewayID, app)
	if _, err := a.applyPersistedFeishuRuntime(persisted, gatewayID, app); err != nil {
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
	cancelFeishuRegistrationRunLocked(session)
	return feishuOnboardingSessionToView(session)
}

func daemonStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
