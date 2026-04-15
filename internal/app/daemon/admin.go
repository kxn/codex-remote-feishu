package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/adminauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/adminui"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type AdminRuntimeOptions struct {
	ConfigPath           string
	LoadConfig           func() (config.LoadedAppConfig, error)
	Services             config.ServicesConfig
	AdminListenHost      string
	AdminListenPort      string
	AdminURL             string
	SetupURL             string
	SSHSession           bool
	SetupRequired        bool
	EnvOverrideActive    bool
	EnvOverrideGatewayID string
}

type adminRuntimeState struct {
	loadConfig           func() (config.LoadedAppConfig, error)
	services             config.ServicesConfig
	adminListenHost      string
	adminListenPort      string
	adminURL             string
	setupURL             string
	sshSession           bool
	setupRequired        bool
	envOverrideActive    bool
	envOverrideGatewayID string
}

type requestAuthState struct {
	Authenticated   bool
	TrustedLoopback bool
	Scope           adminauth.Scope
	ExpiresAt       time.Time
}

type apiErrorPayload struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
	Details   any    `json:"details,omitempty"`
}

type bootstrapStatePayload struct {
	Phase         string                  `json:"phase"`
	SetupRequired bool                    `json:"setupRequired"`
	SSHSession    bool                    `json:"sshSession"`
	Session       bootstrapSessionPayload `json:"session"`
	Config        bootstrapConfigPayload  `json:"config"`
	Relay         bootstrapRelayPayload   `json:"relay"`
	Admin         bootstrapAdminPayload   `json:"admin"`
	Feishu        bootstrapFeishuPayload  `json:"feishu"`
	Gateways      []feishu.GatewayStatus  `json:"gateways,omitempty"`
}

type bootstrapSessionPayload struct {
	Authenticated   bool       `json:"authenticated"`
	TrustedLoopback bool       `json:"trustedLoopback"`
	Scope           string     `json:"scope,omitempty"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
}

type bootstrapConfigPayload struct {
	Path    string `json:"path"`
	Version int    `json:"version"`
}

type bootstrapRelayPayload struct {
	ListenHost string `json:"listenHost"`
	ListenPort string `json:"listenPort"`
	ServerURL  string `json:"serverURL"`
}

type bootstrapAdminPayload struct {
	ListenHost          string     `json:"listenHost"`
	ListenPort          string     `json:"listenPort"`
	URL                 string     `json:"url"`
	SetupURL            string     `json:"setupURL,omitempty"`
	SetupTokenRequired  bool       `json:"setupTokenRequired"`
	SetupTokenExpiresAt *time.Time `json:"setupTokenExpiresAt,omitempty"`
}

type bootstrapFeishuPayload struct {
	AppCount              int `json:"appCount"`
	EnabledAppCount       int `json:"enabledAppCount"`
	ConfiguredAppCount    int `json:"configuredAppCount"`
	RuntimeConfiguredApps int `json:"runtimeConfiguredApps"`
}

type setupCompleteResponse struct {
	SetupRequired bool   `json:"setupRequired"`
	AdminURL      string `json:"adminURL"`
	Message       string `json:"message"`
}

type adminConfigResponse struct {
	Path   string          `json:"path"`
	Config adminConfigView `json:"config"`
}

type adminConfigView struct {
	Version int                     `json:"version"`
	Relay   config.RelaySettings    `json:"relay"`
	Admin   config.AdminSettings    `json:"admin"`
	Tool    config.ToolSettings     `json:"tool,omitempty"`
	Wrapper config.WrapperSettings  `json:"wrapper"`
	Feishu  adminFeishuSettingsView `json:"feishu"`
	Debug   config.DebugSettings    `json:"debug"`
	Storage config.StorageSettings  `json:"storage,omitempty"`
}

type adminFeishuSettingsView struct {
	UseSystemProxy bool                 `json:"useSystemProxy"`
	Apps           []adminFeishuAppView `json:"apps,omitempty"`
}

type adminFeishuAppView struct {
	ID         string     `json:"id,omitempty"`
	Name       string     `json:"name,omitempty"`
	AppID      string     `json:"appId,omitempty"`
	HasSecret  bool       `json:"hasSecret"`
	Enabled    bool       `json:"enabled"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
}

type runtimeStatusPayload struct {
	Instances          []*state.InstanceRecord         `json:"instances"`
	Surfaces           []*state.SurfaceConsoleRecord   `json:"surfaces"`
	Gateways           []feishu.GatewayStatus          `json:"gateways,omitempty"`
	PendingRemoteTurns []orchestrator.RemoteTurnStatus `json:"pendingRemoteTurns"`
	ActiveRemoteTurns  []orchestrator.RemoteTurnStatus `json:"activeRemoteTurns"`
}

func (a *App) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.Handle("GET /assets/", http.FileServerFS(adminui.FS()))
	mux.HandleFunc("GET /", a.handleRootPage)
	mux.HandleFunc("GET /setup", a.handleSetupPage)
	mux.HandleFunc("GET /api/setup/bootstrap-state", a.requireSetup(a.handleBootstrapState))
	mux.HandleFunc("POST /api/setup/complete", a.requireSetup(a.handleSetupComplete))
	mux.HandleFunc("GET /api/setup/feishu/manifest", a.requireSetup(a.handleFeishuManifest))
	mux.HandleFunc("GET /api/setup/feishu/apps", a.requireSetup(a.handleFeishuAppsList))
	mux.HandleFunc("POST /api/setup/feishu/apps", a.requireSetup(a.handleFeishuAppCreate))
	mux.HandleFunc("POST /api/setup/feishu/onboarding/sessions", a.requireSetup(a.handleFeishuOnboardingSessionCreate))
	mux.HandleFunc("GET /api/setup/feishu/onboarding/sessions/{id}", a.requireSetup(a.handleFeishuOnboardingSessionGet))
	mux.HandleFunc("POST /api/setup/feishu/onboarding/sessions/{id}/complete", a.requireSetup(a.handleFeishuOnboardingSessionComplete))
	mux.HandleFunc("PUT /api/setup/feishu/apps/{id}", a.requireSetup(a.handleFeishuAppUpdate))
	mux.HandleFunc("DELETE /api/setup/feishu/apps/{id}", a.requireSetup(a.handleFeishuAppDelete))
	mux.HandleFunc("POST /api/setup/feishu/apps/{id}/verify", a.requireSetup(a.handleFeishuAppVerify))
	mux.HandleFunc("POST /api/setup/feishu/apps/{id}/publish-check", a.requireSetup(a.handleFeishuAppPublishCheck))
	mux.HandleFunc("POST /api/setup/feishu/apps/{id}/reconnect", a.requireSetup(a.handleFeishuAppReconnect))
	mux.HandleFunc("POST /api/setup/feishu/apps/{id}/enable", a.requireSetup(a.handleFeishuAppEnable))
	mux.HandleFunc("POST /api/setup/feishu/apps/{id}/disable", a.requireSetup(a.handleFeishuAppDisable))
	mux.HandleFunc("PATCH /api/setup/feishu/apps/{id}/wizard", a.requireSetup(a.handleFeishuAppWizardUpdate))
	mux.HandleFunc("GET /api/setup/feishu/apps/{id}/scopes-json", a.requireSetup(a.handleFeishuAppScopesJSON))
	mux.HandleFunc("GET /api/setup/runtime-requirements/detect", a.requireSetup(a.handleRuntimeRequirementsDetect))
	mux.HandleFunc("GET /api/setup/autostart/detect", a.requireSetup(a.handleAutostartDetect))
	mux.HandleFunc("POST /api/setup/autostart/apply", a.requireSetup(a.handleAutostartApply))
	mux.HandleFunc("GET /api/setup/vscode/detect", a.requireSetup(a.handleVSCodeDetect))
	mux.HandleFunc("POST /api/setup/vscode/apply", a.requireSetup(a.handleVSCodeApply))
	mux.HandleFunc("POST /api/setup/vscode/reinstall-shim", a.requireSetup(a.handleVSCodeReinstallShim))

	mux.HandleFunc("GET /api/admin/bootstrap-state", a.requireAdmin(a.handleBootstrapState))
	mux.HandleFunc("GET /api/admin/runtime-status", a.requireAdmin(a.handleRuntimeStatus))
	mux.HandleFunc("GET /api/admin/config", a.requireAdmin(a.handleAdminConfig))
	mux.HandleFunc("PUT /api/admin/config", a.requireAdmin(a.handleNotImplemented("PUT /api/admin/config")))
	mux.HandleFunc("GET /api/admin/external-access/status", a.requireAdmin(a.handleAdminExternalAccessStatus))
	mux.HandleFunc("POST /api/admin/external-access/link", a.requireAdmin(a.handleAdminExternalAccessLink))
	mux.HandleFunc("GET /api/admin/feishu/manifest", a.requireAdmin(a.handleFeishuManifest))
	mux.HandleFunc("GET /api/admin/feishu/apps", a.requireAdmin(a.handleFeishuAppsList))
	mux.HandleFunc("POST /api/admin/feishu/apps", a.requireAdmin(a.handleFeishuAppCreate))
	mux.HandleFunc("POST /api/admin/feishu/onboarding/sessions", a.requireAdmin(a.handleFeishuOnboardingSessionCreate))
	mux.HandleFunc("GET /api/admin/feishu/onboarding/sessions/{id}", a.requireAdmin(a.handleFeishuOnboardingSessionGet))
	mux.HandleFunc("POST /api/admin/feishu/onboarding/sessions/{id}/complete", a.requireAdmin(a.handleFeishuOnboardingSessionComplete))
	mux.HandleFunc("PUT /api/admin/feishu/apps/{id}", a.requireAdmin(a.handleFeishuAppUpdate))
	mux.HandleFunc("DELETE /api/admin/feishu/apps/{id}", a.requireAdmin(a.handleFeishuAppDelete))
	mux.HandleFunc("POST /api/admin/feishu/apps/{id}/verify", a.requireAdmin(a.handleFeishuAppVerify))
	mux.HandleFunc("POST /api/admin/feishu/apps/{id}/publish-check", a.requireAdmin(a.handleFeishuAppPublishCheck))
	mux.HandleFunc("POST /api/admin/feishu/apps/{id}/reconnect", a.requireAdmin(a.handleFeishuAppReconnect))
	mux.HandleFunc("POST /api/admin/feishu/apps/{id}/retry-apply", a.requireAdmin(a.handleFeishuAppRetryApply))
	mux.HandleFunc("POST /api/admin/feishu/apps/{id}/enable", a.requireAdmin(a.handleFeishuAppEnable))
	mux.HandleFunc("POST /api/admin/feishu/apps/{id}/disable", a.requireAdmin(a.handleFeishuAppDisable))
	mux.HandleFunc("PATCH /api/admin/feishu/apps/{id}/wizard", a.requireAdmin(a.handleFeishuAppWizardUpdate))
	mux.HandleFunc("GET /api/admin/feishu/apps/{id}/scopes-json", a.requireAdmin(a.handleFeishuAppScopesJSON))
	mux.HandleFunc("GET /api/admin/instances", a.requireAdmin(a.handleAdminInstancesList))
	mux.HandleFunc("POST /api/admin/instances", a.requireAdmin(a.handleAdminInstanceCreate))
	mux.HandleFunc("DELETE /api/admin/instances/{id}", a.requireAdmin(a.handleAdminInstanceDelete))
	mux.HandleFunc("GET /api/admin/storage/image-staging", a.requireAdmin(a.handleImageStagingStatus))
	mux.HandleFunc("POST /api/admin/storage/image-staging/cleanup", a.requireAdmin(a.handleImageStagingCleanup))
	mux.HandleFunc("GET /api/admin/storage/preview-drive/{id}", a.requireAdmin(a.handlePreviewDriveStatus))
	mux.HandleFunc("POST /api/admin/storage/preview-drive/{id}/cleanup", a.requireAdmin(a.handlePreviewDriveCleanup))
	mux.HandleFunc("GET /api/admin/runtime-requirements/detect", a.requireAdmin(a.handleRuntimeRequirementsDetect))
	mux.HandleFunc("GET /api/admin/autostart/detect", a.requireAdmin(a.handleAutostartDetect))
	mux.HandleFunc("POST /api/admin/autostart/apply", a.requireAdmin(a.handleAutostartApply))
	mux.HandleFunc("GET /api/admin/vscode/detect", a.requireAdmin(a.handleVSCodeDetect))
	mux.HandleFunc("POST /api/admin/vscode/apply", a.requireAdmin(a.handleVSCodeApply))
	mux.HandleFunc("POST /api/admin/vscode/reinstall-shim", a.requireAdmin(a.handleVSCodeReinstallShim))
	mux.HandleFunc("GET /v1/status", a.requireAdmin(a.handleStatus))
}

func (a *App) ConfigureAdmin(opts AdminRuntimeOptions) {
	loadConfig := opts.LoadConfig
	if loadConfig == nil {
		configPath := strings.TrimSpace(opts.ConfigPath)
		if configPath != "" {
			loadConfig = func() (config.LoadedAppConfig, error) {
				return config.LoadAppConfigAtPath(configPath)
			}
		} else {
			loadConfig = config.LoadAppConfig
		}
	}

	a.mu.Lock()
	a.admin = adminRuntimeState{
		loadConfig:           loadConfig,
		services:             opts.Services,
		adminListenHost:      opts.AdminListenHost,
		adminListenPort:      opts.AdminListenPort,
		adminURL:             opts.AdminURL,
		setupURL:             opts.SetupURL,
		sshSession:           opts.SSHSession,
		setupRequired:        opts.SetupRequired,
		envOverrideActive:    opts.EnvOverrideActive,
		envOverrideGatewayID: opts.EnvOverrideGatewayID,
	}
	a.mu.Unlock()
}

func (a *App) adminPrefixMux(base http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin":
			http.Redirect(w, r, "/admin/", http.StatusFound)
			return
		case "/admin/":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			a.handleAdminPage(w, r)
			return
		}

		if !strings.HasPrefix(r.URL.Path, "/admin/") {
			http.NotFound(w, r)
			return
		}

		rewritten := r.Clone(r.Context())
		rewritten.URL.Path = strings.TrimPrefix(r.URL.Path, "/admin")
		if rewritten.URL.Path == "" {
			rewritten.URL.Path = "/"
		}
		if rawPath := strings.TrimPrefix(r.URL.RawPath, "/admin"); rawPath != "" {
			rewritten.URL.RawPath = rawPath
		}
		rewritten.RequestURI = ""
		base.ServeHTTP(w, rewritten)
	})
}

func (a *App) EnableSetupAccess(ttl time.Duration) (string, time.Time, error) {
	token, expiresAt, err := a.adminAuth.EnableSetupToken(ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (a *App) DisableSetupAccess() {
	a.adminAuth.DisableSetupToken()
}

func (a *App) handleRootPage(w http.ResponseWriter, r *http.Request) {
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		http.Redirect(w, r, "/setup?token="+url.QueryEscape(token), http.StatusSeeOther)
		return
	}
	writeRootHelpPage(w)
}

func (a *App) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		http.Redirect(w, r, "/setup?token="+url.QueryEscape(token), http.StatusSeeOther)
		return
	}

	auth := a.requestAuth(r)
	if !a.authAllowsAdmin(auth) {
		writePageUnauthorized(w, "admin access is limited to localhost in this stage")
		return
	}
	writeAdminAppShell(w)
}

func (a *App) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		value, expiresAt, err := a.adminAuth.ExchangeSetupToken(token)
		if err != nil {
			http.SetCookie(w, adminauth.ExpiredSessionCookie())
			writePageError(w, http.StatusUnauthorized, "exchange setup token", err)
			return
		}
		http.SetCookie(w, adminauth.SessionCookie(value, expiresAt))
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	auth := a.requestAuth(r)
	if !a.authAllowsSetup(auth) {
		writePageUnauthorized(w, "setup access requires the startup token link or localhost access")
		return
	}
	if _, err := a.bootstrapState(auth); err != nil {
		writePageError(w, http.StatusInternalServerError, "load bootstrap state", err)
		return
	}
	writeAdminAppShell(w)
}

func (a *App) handleBootstrapState(w http.ResponseWriter, r *http.Request) {
	payload, err := a.bootstrapState(a.requestAuth(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "bootstrap_state_unavailable",
			Message: "failed to load bootstrap state",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	state, err := a.bootstrapState(a.requestAuth(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "bootstrap_state_unavailable",
			Message: "failed to load bootstrap state",
			Details: err.Error(),
		})
		return
	}
	if state.SetupRequired {
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "setup_incomplete",
			Message: "setup cannot be completed before at least one runtime feishu app is configured",
		})
		return
	}

	a.DisableSetupAccess()
	http.SetCookie(w, adminauth.ExpiredSessionCookie())

	message := "setup access disabled; continue in the local admin page"
	if state.SSHSession {
		message = "setup access disabled; remote admin remains limited to localhost in this stage"
	}
	writeJSON(w, http.StatusOK, setupCompleteResponse{
		SetupRequired: false,
		AdminURL:      state.Admin.URL,
		Message:       message,
	})
}

func (a *App) handleRuntimeStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.runtimeStatusPayload())
}

func (a *App) handleAdminConfig(w http.ResponseWriter, _ *http.Request) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, adminConfigResponse{
		Path:   loaded.Path,
		Config: redactAdminConfig(loaded.Config),
	})
}

func (a *App) handleNotImplemented(endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeAPIError(w, http.StatusNotImplemented, apiError{
			Code:    "not_implemented",
			Message: "admin endpoint is not implemented yet",
			Details: map[string]any{
				"endpoint": endpoint,
			},
		})
	}
}

func (a *App) requireSetup(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := a.requestAuth(r)
		if !a.authAllowsSetup(auth) {
			writeAPIError(w, http.StatusUnauthorized, apiError{
				Code:    "setup_auth_required",
				Message: "setup access requires localhost or a valid setup session",
			})
			return
		}
		next(w, r)
	}
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := a.requestAuth(r)
		if !a.authAllowsAdmin(auth) {
			writeAPIError(w, http.StatusUnauthorized, apiError{
				Code:    "admin_auth_required",
				Message: "admin access is currently limited to localhost",
			})
			return
		}
		next(w, r)
	}
}

func (a *App) requestAuth(r *http.Request) requestAuthState {
	if adminauth.IsLoopbackRequest(r) {
		return requestAuthState{
			Authenticated:   true,
			TrustedLoopback: true,
			Scope:           adminauth.ScopeAdmin,
		}
	}

	cookie, err := r.Cookie(adminauth.CookieName)
	if err != nil {
		return requestAuthState{}
	}
	session, err := a.adminAuth.ParseSession(cookie.Value)
	if err != nil {
		return requestAuthState{}
	}
	return requestAuthState{
		Authenticated: true,
		Scope:         session.Scope,
		ExpiresAt:     session.ExpiresAt,
	}
}

func (a *App) authAllowsSetup(auth requestAuthState) bool {
	return auth.TrustedLoopback || auth.Scope == adminauth.ScopeSetup || auth.Scope == adminauth.ScopeAdmin
}

func (a *App) authAllowsAdmin(auth requestAuthState) bool {
	return auth.TrustedLoopback || auth.Scope == adminauth.ScopeAdmin
}

func (a *App) bootstrapState(auth requestAuthState) (bootstrapStatePayload, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return bootstrapStatePayload{}, err
	}

	a.mu.Lock()
	admin := a.admin
	a.mu.Unlock()

	setupRequired := requiresSetup(loaded.Config, admin.services)
	setupEnabled, setupExpiresAt := a.adminAuth.SetupStatus()
	gateways := gatewayStatuses(a.gateway)
	enabledCount := 0
	configuredCount := 0
	for _, app := range loaded.Config.Feishu.Apps {
		if app.Enabled == nil || *app.Enabled {
			enabledCount++
		}
		if strings.TrimSpace(app.AppID) != "" && strings.TrimSpace(app.AppSecret) != "" {
			configuredCount++
		}
	}

	var expiresAt *time.Time
	if auth.ExpiresAt.After(time.Time{}) {
		value := auth.ExpiresAt.UTC()
		expiresAt = &value
	}
	var setupTokenExpiresAt *time.Time
	if setupEnabled {
		value := setupExpiresAt.UTC()
		setupTokenExpiresAt = &value
	}

	return bootstrapStatePayload{
		Phase:         bootstrapPhase(setupRequired, gateways),
		SetupRequired: setupRequired,
		SSHSession:    admin.sshSession,
		Session: bootstrapSessionPayload{
			Authenticated:   auth.Authenticated,
			TrustedLoopback: auth.TrustedLoopback,
			Scope:           string(auth.Scope),
			ExpiresAt:       expiresAt,
		},
		Config: bootstrapConfigPayload{
			Path:    loaded.Path,
			Version: loaded.Config.Version,
		},
		Relay: bootstrapRelayPayload{
			ListenHost: admin.services.RelayHost,
			ListenPort: admin.services.RelayPort,
			ServerURL:  loaded.Config.Relay.ServerURL,
		},
		Admin: bootstrapAdminPayload{
			ListenHost:          admin.adminListenHost,
			ListenPort:          admin.adminListenPort,
			URL:                 admin.adminURL,
			SetupURL:            admin.setupURL,
			SetupTokenRequired:  setupRequired && !a.authAllowsSetup(auth),
			SetupTokenExpiresAt: setupTokenExpiresAt,
		},
		Feishu: bootstrapFeishuPayload{
			AppCount:              len(loaded.Config.Feishu.Apps),
			EnabledAppCount:       enabledCount,
			ConfiguredAppCount:    configuredCount,
			RuntimeConfiguredApps: configuredRuntimeAppCount(loaded.Config, admin.services),
		},
		Gateways: gateways,
	}, nil
}

func (a *App) loadAdminConfig() (config.LoadedAppConfig, error) {
	loadConfig := a.admin.loadConfig
	if loadConfig == nil {
		return config.LoadAppConfig()
	}
	return loadConfig()
}

func gatewayStatuses(gateway feishu.Gateway) []feishu.GatewayStatus {
	statusSource, ok := gateway.(interface{ Status() []feishu.GatewayStatus })
	if !ok {
		return nil
	}
	return statusSource.Status()
}

func bootstrapPhase(setupRequired bool, gateways []feishu.GatewayStatus) string {
	if setupRequired {
		return "uninitialized"
	}
	hasConnected := false
	for _, gateway := range gateways {
		switch gateway.State {
		case feishu.GatewayStateConnected:
			hasConnected = true
		case feishu.GatewayStateDisabled:
		default:
			return "ready_degraded"
		}
	}
	if !hasConnected {
		return "ready_degraded"
	}
	return "ready"
}

func redactAdminConfig(cfg config.AppConfig) adminConfigView {
	view := adminConfigView{
		Version: cfg.Version,
		Relay:   cfg.Relay,
		Admin:   cfg.Admin,
		Tool:    cfg.Tool,
		Wrapper: cfg.Wrapper,
		Debug:   cfg.Debug,
		Storage: cfg.Storage,
		Feishu: adminFeishuSettingsView{
			UseSystemProxy: cfg.Feishu.UseSystemProxy,
		},
	}
	for _, app := range cfg.Feishu.Apps {
		view.Feishu.Apps = append(view.Feishu.Apps, adminFeishuAppView{
			ID:         app.ID,
			Name:       app.Name,
			AppID:      app.AppID,
			HasSecret:  strings.TrimSpace(app.AppSecret) != "",
			Enabled:    app.Enabled == nil || *app.Enabled,
			VerifiedAt: app.VerifiedAt,
		})
	}
	return view
}

func (a *App) runtimeStatusPayload() runtimeStatusPayload {
	a.mu.Lock()
	defer a.mu.Unlock()
	return runtimeStatusPayload{
		Instances:          a.service.Instances(),
		Surfaces:           a.service.Surfaces(),
		Gateways:           gatewayStatuses(a.gateway),
		PendingRemoteTurns: a.service.PendingRemoteTurns(),
		ActiveRemoteTurns:  a.service.ActiveRemoteTurns(),
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAPIError(w http.ResponseWriter, status int, apiErr apiError) {
	writeJSON(w, status, apiErrorPayload{Error: apiErr})
}

func writePageUnauthorized(w http.ResponseWriter, message string) {
	writePageError(w, http.StatusUnauthorized, "access denied", errors.New(message))
}

func writePageError(w http.ResponseWriter, status int, title string, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><html><body style=\"font-family: sans-serif; padding: 32px;\"><h1>%s</h1><p>%s</p></body></html>", html.EscapeString(title), html.EscapeString(err.Error()))
}

func writeRootHelpPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, `<!doctype html><html><body style="font-family: sans-serif; padding: 32px;">
<h1>Codex Remote</h1>
<p>Admin entry has moved to <a href="/admin/">/admin/</a>.</p>
<p>Setup remains available at <a href="/setup">/setup</a>.</p>
<p>This root page is intentionally lightweight so external access can keep separate module prefixes.</p>
</body></html>`)
}

func writeAdminAppShell(w http.ResponseWriter) {
	indexHTML, err := adminui.IndexHTML()
	if err != nil {
		writePageError(w, http.StatusInternalServerError, "admin ui unavailable", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexHTML)
}
