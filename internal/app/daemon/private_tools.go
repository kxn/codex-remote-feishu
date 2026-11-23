package daemon

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/toolservicecontract"
)

// Installed once before the HTTP listener starts. Both applications can exist
// in one daemon, but only an authenticated private carrier gets this service.
type privateToolRuntime struct{ host *privateNativeHost }
type privateToolCaller struct {
	runtime *privateToolRuntime
	record  privateCarrierRecord
}
type privateToolCallerKey struct{}
type privateToolSurfaceKey struct{}

func (r *privateToolRuntime) authenticate(ctx context.Context, instance, bearer string) (*privateToolCaller, error) {
	if r == nil || r.host == nil || instance == "" || bearer == "" {
		return nil, fmt.Errorf("private tool identity required")
	}
	rec, err := r.host.store.lookup(ctx, instance)
	if err != nil {
		return nil, err
	}
	if rec.State != "running" {
		return nil, fmt.Errorf("private carrier is not running")
	}
	credential, err := r.host.store.credential(ctx, rec)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(bearer), []byte(toolservicecontract.PrivateBearer(credential))) != 1 {
		return nil, fmt.Errorf("invalid private tool credential")
	}
	if err = r.host.registry.WithLease(ctx, privateRegistration(rec.Snapshot.Owner), func() error { return nil }); err != nil {
		return nil, err
	}
	return &privateToolCaller{r, rec}, nil
}

func (c *privateToolCaller) run(ctx context.Context, call func(context.Context) error) error {
	h := c.runtime.host
	return h.outbound.Send(ctx, privateRegistration(c.record.Snapshot.Owner), func() error {
		current, err := h.store.lookup(ctx, c.record.Snapshot.Owner.InstanceID)
		if err != nil {
			return err
		}
		if current.Snapshot != c.record.Snapshot || current.State != "running" {
			return fmt.Errorf("private tool carrier changed")
		}
		return nil
	}, func() error {
		// The registry transaction has ended before taking the processor lock.
		// Outbound remains active through the actual SDK request, so retirement
		// cannot transfer this carrier while its file is being published.
		s, err := h.processor.ToolSurface(ctx, c.record.Surface, c.record.SurfaceGeneration, c.record.Snapshot.Owner.InstanceID)
		if err != nil {
			return err
		}
		resolved := resolvedToolSurfaceContext{SurfaceSessionID: s.Owner.SurfaceID, Platform: "feishu", GatewayID: s.Owner.GatewayID, ChatID: s.Owner.ChatID, ActorUserID: s.Owner.UserID, ProductMode: string(state.ProductModeNormal), AttachedInstanceID: s.InstanceID, SelectedThreadID: s.ThreadID, RouteMode: s.RouteMode, WorkspaceKey: s.WorkspaceKey, WorkspaceRoot: s.WorkspaceRoot, InstanceSource: s.InstanceSource, InstanceManaged: s.Managed, Attached: true}
		return call(context.WithValue(ctx, privateToolSurfaceKey{}, resolved))
	})
}

func (a *App) authenticatePrivateTool(w http.ResponseWriter, r *http.Request, next http.Handler) {
	instance := strings.TrimSpace(r.URL.Query().Get(toolservicecontract.CallerInstanceIDQueryParam))
	bearer := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	caller, err := a.privateTools.authenticate(r.Context(), instance, bearer)
	if err != nil {
		writeToolError(w, http.StatusUnauthorized, toolError{Code: "invalid_private_tool_identity", Message: "The tool caller is not an active private-chat agent."})
		return
	}
	ctx := withToolCallerInstanceID(context.WithValue(r.Context(), privateToolCallerKey{}, caller), instance)
	next.ServeHTTP(w, r.WithContext(ctx))
}

func (a *App) resolveToolCallerSurfaceContext(ctx context.Context) (resolvedToolSurfaceContext, *toolError) {
	if resolved, ok := ctx.Value(privateToolSurfaceKey{}).(resolvedToolSurfaceContext); ok {
		return resolved, nil
	}
	if a.privateTools != nil {
		return resolvedToolSurfaceContext{}, &toolError{Code: "private_turn_required", Message: "This tool is available only during its authenticated private-chat turn."}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resolveToolCallerSurfaceContextLocked(toolCallerInstanceIDFromContext(ctx))
}

func (c *privateToolCaller) profileInstance() *state.InstanceRecord {
	return &state.InstanceRecord{InstanceID: c.record.Snapshot.Owner.InstanceID, Backend: c.record.Snapshot.Owner.Backend, CodexProfileID: c.record.CodexProfileID, ClaudeProfileID: c.record.ClaudeProfileID}
}

func (a *App) callerContextProfileVisionSupported(ctx context.Context) bool {
	if caller, ok := ctx.Value(privateToolCallerKey{}).(*privateToolCaller); ok {
		loaded, err := a.loadAdminConfig()
		return err == nil && instanceProfileVisionSupported(loaded.Config, caller.profileInstance())
	}
	if a.privateTools != nil {
		return false
	}
	return a.callerInstanceProfileVisionSupported(toolCallerInstanceIDFromContext(ctx))
}
