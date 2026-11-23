package daemon

import (
	"context"
	"errors"
	"net/http"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
)

var errApplicationClosing = errors.New("application runtime is shutting down")

// A gateway timeout is not proof that its callback finished. Protect every raw
// SDK entry as well as HTTP/relay handlers until their database work returns.
func (a *App) guardApplicationIngress(h feishu.RawIngressHook) feishu.RawIngressHook {
	h.Authorize = guardApplicationResult(a, h.Authorize)
	h.Receive = guardApplicationCall(a, h.Receive)
	h.ReceiveInteraction = guardApplicationResult(a, h.ReceiveInteraction)
	h.ReceiveInteractionResponse = guardApplicationResult(a, h.ReceiveInteractionResponse)
	h.ReceiveMenu = guardApplicationCall(a, h.ReceiveMenu)
	h.ReceivePlatformEvent = guardApplicationCall(a, h.ReceivePlatformEvent)
	return h
}

func guardApplicationResult[T, R any](a *App, next func(context.Context, T) (R, error)) func(context.Context, T) (R, error) {
	if next == nil {
		return nil
	}
	return func(ctx context.Context, value T) (R, error) {
		if a.applications != nil {
			endPlatform, ok := a.applications.platformDeliveries.begin()
			if !ok {
				var zero R
				return zero, errApplicationClosing
			}
			defer endPlatform()
		}
		done, ok := a.beginApplicationNativeDelivery()
		if !ok {
			var zero R
			return zero, errApplicationClosing
		}
		defer done()
		return next(ctx, value)
	}
}

func guardApplicationCall[T any](a *App, next func(context.Context, T) error) func(context.Context, T) error {
	if next == nil {
		return nil
	}
	guarded := guardApplicationResult(a, func(ctx context.Context, value T) (struct{}, error) {
		return struct{}{}, next(ctx, value)
	})
	return func(ctx context.Context, value T) error {
		_, err := guarded(ctx, value)
		return err
	}
}

// This counter protects store lifetime, not execution ownership. The selected
// application still checks every event, HTTP request and tool call's owner.
func (a *App) beginApplicationNativeDelivery() (func(), bool) {
	r := a.applications
	if r == nil {
		return func() {}, true
	}
	return r.deliveries.begin()
}

func (a *App) applicationRequestGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done, ok := a.beginApplicationNativeDelivery()
		if !ok {
			http.Error(w, "Service is shutting down", http.StatusServiceUnavailable)
			return
		}
		defer done()
		next.ServeHTTP(w, r)
	})
}

func (r *applicationRuntime) drainNativeDeliveries(ctx context.Context) error {
	return r.deliveries.drain(ctx)
}
