package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess"
)

const defaultPreviewGrantTTL = 24 * time.Hour

type previewGrantRecord struct {
	ExternalURL string
	ExpiresAt   time.Time
}

type daemonWebPreviewPublisher struct {
	app *App
}

func (p daemonWebPreviewPublisher) IssueScopePrefix(ctx context.Context, req previewpkg.WebPreviewGrantRequest) (string, error) {
	if p.app == nil {
		return "", fmt.Errorf("preview publisher app is not configured")
	}
	return p.app.issuePreviewScopePrefix(ctx, req)
}

func (a *App) handlePreviewPage(w http.ResponseWriter, r *http.Request) {
	a.servePreviewRoute(w, r, false)
}

func (a *App) handlePreviewScopeRoot(w http.ResponseWriter, r *http.Request) {
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
	routeService, ok := a.finalBlockPreviewer.(previewpkg.WebPreviewRouteService)
	if !ok || !routeService.ServeWebPreview(w, r, scopePublicID, previewID, download) {
		http.NotFound(w, r)
	}
}

func (a *App) issuePreviewScopePrefix(ctx context.Context, req previewpkg.WebPreviewGrantRequest) (string, error) {
	scopePublicID := strings.TrimSpace(req.ScopePublicID)
	if scopePublicID == "" {
		return "", fmt.Errorf("preview scope id is required")
	}
	cacheKey := previewGrantCacheKey(scopePublicID, req.GrantKey)
	if cacheKey == "" {
		return "", fmt.Errorf("preview grant key is required")
	}
	now := time.Now().UTC()

	a.mu.Lock()
	if grant := a.webPreviewGrants[cacheKey]; grant != nil && strings.TrimSpace(grant.ExternalURL) != "" && grant.ExpiresAt.After(now) {
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
	a.webPreviewGrants[cacheKey] = &previewGrantRecord{
		ExternalURL: issued.ExternalURL,
		ExpiresAt:   issued.ExpiresAt,
	}
	a.mu.Unlock()
	return issued.ExternalURL, nil
}

func (a *App) previewGrantKey(gatewayID, surfaceSessionID string, block render.Block) string {
	parts := []string{
		strings.TrimSpace(gatewayID),
		strings.TrimSpace(surfaceSessionID),
		strings.TrimSpace(block.ThreadID),
		strings.TrimSpace(block.TurnID),
		strings.TrimSpace(block.ItemID),
		strings.TrimSpace(block.ID),
	}
	for _, part := range parts[3:] {
		if part != "" {
			return "message|" + strings.Join(parts, "|")
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(append(parts, strings.TrimSpace(block.Text)), "|")))
	return "message|fallback|" + hex.EncodeToString(sum[:8])
}

func previewGrantCacheKey(scopePublicID, grantKey string) string {
	scopePublicID = strings.TrimSpace(scopePublicID)
	grantKey = strings.TrimSpace(grantKey)
	if scopePublicID == "" || grantKey == "" {
		return ""
	}
	return scopePublicID + "|" + grantKey
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
