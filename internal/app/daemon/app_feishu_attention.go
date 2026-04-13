package daemon

import (
	"context"
	"log"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type feishuTimeSensitiveState struct {
	GatewayID     string
	ReceiveID     string
	ReceiveIDType string
}

func (s feishuTimeSensitiveState) operation(surfaceID string, enabled bool) feishu.Operation {
	return feishu.Operation{
		Kind:             feishu.OperationSetTimeSensitive,
		GatewayID:        s.GatewayID,
		SurfaceSessionID: surfaceID,
		ReceiveID:        s.ReceiveID,
		ReceiveIDType:    s.ReceiveIDType,
		TimeSensitive:    enabled,
	}
}

func (a *App) syncFeishuTimeSensitiveLocked(ctx context.Context) {
	seen := make(map[string]bool, len(a.feishuTimeSensitive))
	for _, surface := range a.service.Surfaces() {
		target, enabled, ok := feishuTimeSensitiveTarget(surface)
		if !ok {
			continue
		}
		surfaceID := strings.TrimSpace(surface.SurfaceSessionID)
		seen[surfaceID] = true
		cached, wasEnabled := a.feishuTimeSensitive[surfaceID]
		switch {
		case enabled && wasEnabled:
			continue
		case enabled:
			if err := a.applyFeishuTimeSensitiveLocked(ctx, target.operation(surfaceID, true)); err != nil {
				log.Printf("feishu time sensitive enable failed: surface=%s gateway=%s user=%s err=%v", surfaceID, target.GatewayID, target.ReceiveID, err)
				continue
			}
			a.feishuTimeSensitive[surfaceID] = target
		case wasEnabled:
			if err := a.applyFeishuTimeSensitiveLocked(ctx, cached.operation(surfaceID, false)); err != nil {
				log.Printf("feishu time sensitive disable failed: surface=%s gateway=%s user=%s err=%v", surfaceID, cached.GatewayID, cached.ReceiveID, err)
				continue
			}
			delete(a.feishuTimeSensitive, surfaceID)
		}
	}

	for surfaceID, cached := range a.feishuTimeSensitive {
		if seen[surfaceID] {
			continue
		}
		if err := a.applyFeishuTimeSensitiveLocked(ctx, cached.operation(surfaceID, false)); err != nil {
			log.Printf("feishu time sensitive cleanup failed: surface=%s gateway=%s user=%s err=%v", surfaceID, cached.GatewayID, cached.ReceiveID, err)
			continue
		}
		delete(a.feishuTimeSensitive, surfaceID)
	}
}

func (a *App) applyFeishuTimeSensitiveLocked(ctx context.Context, operation feishu.Operation) error {
	applyCtx, applyCancel := a.newTimeoutContext(ctx, a.gatewayApplyTimeout)
	defer applyCancel()
	if err := a.gateway.Apply(applyCtx, []feishu.Operation{operation}); err != nil {
		if a.observeFeishuPermissionError(operation.GatewayID, err) {
			log.Printf("feishu permission gap observed during time-sensitive apply: gateway=%s surface=%s err=%v", operation.GatewayID, operation.SurfaceSessionID, err)
			return nil
		}
		return err
	}
	return nil
}

func feishuTimeSensitiveTarget(surface *state.SurfaceConsoleRecord) (feishuTimeSensitiveState, bool, bool) {
	if surface == nil {
		return feishuTimeSensitiveState{}, false, false
	}
	ref, ok := feishu.ParseSurfaceRef(surface.SurfaceSessionID)
	if !ok || ref.ScopeKind != feishu.ScopeKindUser {
		return feishuTimeSensitiveState{}, false, false
	}
	receiveID, receiveIDType := feishu.ResolveReceiveTarget("", ref.ScopeID)
	if receiveID == "" || receiveIDType == "" {
		return feishuTimeSensitiveState{}, false, false
	}
	return feishuTimeSensitiveState{
		GatewayID:     firstNonEmpty(strings.TrimSpace(surface.GatewayID), ref.GatewayID),
		ReceiveID:     receiveID,
		ReceiveIDType: receiveIDType,
	}, surfaceNeedsUserInput(surface), true
}

func surfaceNeedsUserInput(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	if surface.ActiveRequestCapture != nil || surface.ActiveCommandCapture != nil {
		return true
	}
	for _, request := range surface.PendingRequests {
		if request != nil {
			return true
		}
	}
	return false
}
