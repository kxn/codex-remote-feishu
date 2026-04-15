package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess"
)

const defaultPreviewGrantTTL = 30 * time.Minute

type previewScopeGrant struct {
	ExternalURL string
	ExpiresAt   time.Time
}

type daemonWebPreviewPublisher struct {
	app *App
}

func (p daemonWebPreviewPublisher) IssueScopePrefix(ctx context.Context, scopePublicID string) (string, error) {
	if p.app == nil {
		return "", fmt.Errorf("preview publisher app is not configured")
	}
	return p.app.issuePreviewScopePrefix(ctx, scopePublicID)
}

func (a *App) handlePreviewPage(w http.ResponseWriter, r *http.Request) {
	a.servePreviewRoute(w, r, false)
}

func (a *App) handlePreviewDownload(w http.ResponseWriter, r *http.Request) {
	a.servePreviewRoute(w, r, true)
}

func (a *App) servePreviewRoute(w http.ResponseWriter, r *http.Request, download bool) {
	auth := a.requestAuth(r)
	if !a.authAllowsAdmin(auth) {
		writePageUnauthorized(w, "preview access requires localhost or a valid external preview session")
		return
	}
	scopePublicID := strings.TrimSpace(r.PathValue("scope"))
	previewID := strings.TrimSpace(r.PathValue("preview"))
	routeService, ok := a.finalBlockPreviewer.(feishu.WebPreviewRouteService)
	if !ok || !routeService.ServeWebPreview(w, r, scopePublicID, previewID, download) {
		http.NotFound(w, r)
	}
}

func (a *App) issuePreviewScopePrefix(ctx context.Context, scopePublicID string) (string, error) {
	scopePublicID = strings.TrimSpace(scopePublicID)
	if scopePublicID == "" {
		return "", fmt.Errorf("preview scope id is required")
	}
	now := time.Now().UTC()

	a.mu.Lock()
	if grant := a.webPreviewGrants[scopePublicID]; grant != nil && strings.TrimSpace(grant.ExternalURL) != "" && grant.ExpiresAt.After(now) {
		url := grant.ExternalURL
		a.mu.Unlock()
		return url, nil
	}
	a.mu.Unlock()

	targetURL, targetBasePath, err := a.previewScopeTarget(scopePublicID)
	if err != nil {
		return "", err
	}
	issued, err := a.IssueExternalAccessURL(ctx, externalaccess.IssueRequest{
		Purpose:        externalaccess.PurposePreview,
		TargetURL:      targetURL,
		TargetBasePath: targetBasePath,
		LinkTTL:        defaultPreviewGrantTTL,
		SessionTTL:     defaultPreviewGrantTTL,
	})
	if err != nil {
		return "", err
	}

	a.mu.Lock()
	a.webPreviewGrants[scopePublicID] = &previewScopeGrant{
		ExternalURL: issued.ExternalURL,
		ExpiresAt:   issued.ExpiresAt,
	}
	a.mu.Unlock()
	return issued.ExternalURL, nil
}

func (a *App) previewScopeTarget(scopePublicID string) (string, string, error) {
	baseURL, err := a.previewLocalBaseURL()
	if err != nil {
		return "", "", err
	}
	targetBasePath := "/preview/s/" + scopePublicID + "/"
	return strings.TrimRight(baseURL, "/") + targetBasePath, targetBasePath, nil
}

func (a *App) previewLocalBaseURL() (string, error) {
	a.listenMu.Lock()
	listener := a.apiListener
	a.listenMu.Unlock()
	if listener != nil {
		_, port, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			return "", err
		}
		return "http://" + net.JoinHostPort("127.0.0.1", port), nil
	}

	a.mu.Lock()
	port := strings.TrimSpace(a.admin.adminListenPort)
	a.mu.Unlock()
	if port == "" {
		return "", fmt.Errorf("preview local api port is unavailable")
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port), nil
}
