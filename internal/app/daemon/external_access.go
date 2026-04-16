package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess"
)

type externalAccessStatusResponse struct {
	Status externalaccess.Status `json:"status"`
}

type externalAccessLinkRequest struct {
	Purpose           string `json:"purpose"`
	TargetURL         string `json:"targetURL"`
	TargetBasePath    string `json:"targetBasePath,omitempty"`
	LinkTTLSeconds    int    `json:"linkTTLSeconds,omitempty"`
	SessionTTLSeconds int    `json:"sessionTTLSeconds,omitempty"`
	AllowWebsocket    bool   `json:"allowWebsocket,omitempty"`
}

type externalAccessLinkResponse struct {
	URL externalaccess.IssuedURL `json:"url"`
}

type externalAccessShutdownPlan struct {
	reason   string
	service  *externalaccess.Service
	server   *http.Server
	listener net.Listener
	waitCh   chan struct{}
	waitOnly bool
}

func externalAccessSettingsViewFromConfig(value config.ExternalAccessSettings) externalAccessSettingsView {
	value = config.ResolveExternalAccessSettings(value)
	lazyStart := value.Provider.LazyStart == nil || *value.Provider.LazyStart
	return externalAccessSettingsView{
		ListenHost:                 strings.TrimSpace(value.ListenHost),
		ListenPort:                 value.ListenPort,
		DefaultLinkTTL:             time.Duration(value.DefaultLinkTTLSeconds) * time.Second,
		DefaultSessionTTL:          time.Duration(value.DefaultSessionTTLSeconds) * time.Second,
		ProviderKind:               strings.TrimSpace(value.Provider.Kind),
		ProviderLazyStart:          lazyStart,
		TryCloudflareBinaryPath:    strings.TrimSpace(value.Provider.TryCloudflare.BinaryPath),
		TryCloudflareLaunchTimeout: time.Duration(value.Provider.TryCloudflare.LaunchTimeoutSeconds) * time.Second,
		TryCloudflareMetricsPort:   value.Provider.TryCloudflare.MetricsPort,
		TryCloudflareLogPath:       strings.TrimSpace(value.Provider.TryCloudflare.LogPath),
	}
}

func (a *App) SetExternalAccess(cfg ExternalAccessRuntimeConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.externalAccessRuntime = cfg
	a.externalAccess = newExternalAccessService(cfg)
}

func newExternalAccessService(cfg ExternalAccessRuntimeConfig) *externalaccess.Service {
	settings := cfg.Settings
	var provider externalaccess.Provider
	switch strings.ToLower(strings.TrimSpace(settings.ProviderKind)) {
	case "", "disabled":
		provider = nil
	case "trycloudflare":
		provider = externalaccess.NewTryCloudflareProvider(externalaccess.TryCloudflareOptions{
			BinaryPath:    settings.TryCloudflareBinaryPath,
			CurrentBinary: cfg.CurrentBinary,
			LaunchTimeout: settings.TryCloudflareLaunchTimeout,
			MetricsPort:   settings.TryCloudflareMetricsPort,
			LogPath:       settings.TryCloudflareLogPath,
		})
	default:
		provider = nil
	}
	return externalaccess.NewService(externalaccess.Options{
		Provider:          provider,
		DefaultLinkTTL:    settings.DefaultLinkTTL,
		DefaultSessionTTL: settings.DefaultSessionTTL,
		IdleTTL:           externalaccess.DefaultIdleTTL,
	})
}

func (a *App) handleAdminExternalAccessStatus(w http.ResponseWriter, _ *http.Request) {
	if a.externalAccess == nil {
		writeJSON(w, http.StatusOK, externalAccessStatusResponse{Status: externalaccess.Status{Enabled: false}})
		return
	}
	writeJSON(w, http.StatusOK, externalAccessStatusResponse{Status: a.externalAccess.Snapshot()})
}

func (a *App) handleAdminExternalAccessLink(w http.ResponseWriter, r *http.Request) {
	var payload externalAccessLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_json",
			Message: "request body must be valid JSON",
		})
		return
	}
	issued, err := a.IssueExternalAccessURL(r.Context(), externalaccess.IssueRequest{
		Purpose:        externalaccess.Purpose(strings.TrimSpace(payload.Purpose)),
		TargetURL:      strings.TrimSpace(payload.TargetURL),
		TargetBasePath: strings.TrimSpace(payload.TargetBasePath),
		LinkTTL:        time.Duration(payload.LinkTTLSeconds) * time.Second,
		SessionTTL:     time.Duration(payload.SessionTTLSeconds) * time.Second,
		AllowWebsocket: payload.AllowWebsocket,
	})
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case err == externalaccess.ErrDisabled:
			status = http.StatusConflict
		case err == externalaccess.ErrTargetNotLoopback:
			status = http.StatusBadRequest
		}
		writeAPIError(w, status, apiError{
			Code:    "external_access_issue_failed",
			Message: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusCreated, externalAccessLinkResponse{URL: issued})
}

func (a *App) IssueExternalAccessURL(ctx context.Context, req externalaccess.IssueRequest) (externalaccess.IssuedURL, error) {
	service, localURL, err := a.ensureExternalAccessIssueTarget()
	if err != nil {
		return externalaccess.IssuedURL{}, err
	}
	return service.IssueURL(ctx, req, localURL)
}

func (a *App) ensureExternalAccessIssueTarget() (*externalaccess.Service, string, error) {
	a.mu.Lock()
	service, localURL, err := a.ensureExternalAccessIssueTargetLocked()
	a.mu.Unlock()
	return service, localURL, err
}

func (a *App) ensureExternalAccessIssueTargetLocked() (*externalaccess.Service, string, error) {
	for {
		service := a.externalAccess
		if service == nil {
			return nil, "", externalaccess.ErrDisabled
		}
		if waitCh := a.externalAccessShutdownWait; waitCh != nil {
			a.mu.Unlock()
			<-waitCh
			a.mu.Lock()
			continue
		}
		localURL, err := a.ensureExternalAccessListenerLocked()
		if err != nil {
			return nil, "", err
		}
		return service, localURL, nil
	}
}

func (a *App) ensureExternalAccessListenerLocked() (string, error) {
	if a.externalAccess == nil {
		return "", externalaccess.ErrDisabled
	}
	if a.externalAccessListener != nil {
		return "http://" + a.externalAccessListener.Addr().String(), nil
	}
	settings := a.externalAccessRuntime.Settings
	host := strings.TrimSpace(settings.ListenHost)
	if host == "" {
		host = "127.0.0.1"
	}
	port := settings.ListenPort
	if port < 0 {
		port = 9512
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return "", err
	}
	server := &http.Server{Handler: a.externalAccess}
	a.externalAccessListener = listener
	a.externalAccessServer = server
	localURL := "http://" + listener.Addr().String()
	a.externalAccess.SetListenerState(localURL, true)
	go func(srv *http.Server, ln net.Listener) {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("external access listener failed: %v", err)
		}
	}(server, listener)
	return localURL, nil
}

func (a *App) prepareExternalAccessShutdownLocked(reason string) externalAccessShutdownPlan {
	if a.externalAccess == nil {
		return externalAccessShutdownPlan{}
	}
	if waitCh := a.externalAccessShutdownWait; waitCh != nil {
		return externalAccessShutdownPlan{
			reason:   reason,
			waitCh:   waitCh,
			waitOnly: true,
		}
	}
	waitCh := make(chan struct{})
	server := a.externalAccessServer
	listener := a.externalAccessListener
	service := a.externalAccess
	a.externalAccessServer = nil
	a.externalAccessListener = nil
	a.externalAccessShutdownWait = waitCh
	a.webPreviewGrants = map[string]*previewScopeGrant{}
	return externalAccessShutdownPlan{
		reason:   reason,
		service:  service,
		server:   server,
		listener: listener,
		waitCh:   waitCh,
	}
}

func (a *App) executeExternalAccessShutdownPlan(plan externalAccessShutdownPlan) {
	if plan.waitCh == nil {
		return
	}
	if plan.waitOnly {
		<-plan.waitCh
		return
	}
	defer func() {
		a.mu.Lock()
		if a.externalAccessShutdownWait == plan.waitCh {
			close(plan.waitCh)
			a.externalAccessShutdownWait = nil
		}
		a.mu.Unlock()
	}()
	if plan.server != nil {
		_ = plan.server.Close()
	}
	if plan.listener != nil {
		_ = plan.listener.Close()
	}
	if plan.service != nil {
		if err := plan.service.ShutdownRuntime(); err != nil {
			log.Printf("external access shutdown (%s) failed: %v", plan.reason, err)
		}
	}
}

func (a *App) shutdownExternalAccessLocked(reason string) {
	plan := a.prepareExternalAccessShutdownLocked(reason)
	if plan.waitCh == nil {
		return
	}
	a.mu.Unlock()
	a.executeExternalAccessShutdownPlan(plan)
	a.mu.Lock()
}

func (a *App) maybeShutdownExternalAccessIdleLocked(now time.Time) {
	if a.externalAccess == nil || !a.externalAccess.IdleExpired(now) {
		return
	}
	log.Printf("external access idle timeout reached; shutting down listener and provider")
	a.shutdownExternalAccessLocked("idle_timeout")
}

func debugAdminIssueRequest(adminURL string) externalaccess.IssueRequest {
	return externalaccess.IssueRequest{
		Purpose:        externalaccess.PurposeDebug,
		TargetURL:      strings.TrimRight(strings.TrimSpace(adminURL), "/") + "/",
		TargetBasePath: "/",
		AllowWebsocket: true,
	}
}

func (a *App) issueDebugAdminURL(ctx context.Context) (externalaccess.IssuedURL, error) {
	a.mu.Lock()
	adminURL := a.admin.adminURL
	a.mu.Unlock()
	return a.IssueExternalAccessURL(ctx, debugAdminIssueRequest(adminURL))
}

func normalizeExternalAccessTargetURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("target url host is required")
	}
	return parsed.String(), nil
}
