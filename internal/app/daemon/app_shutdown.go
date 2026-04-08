package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const daemonShutdownNoticeText = "daemon 正在关闭，当前飞书窗口会暂时离线。若稍后完成重启或升级，请重新发送消息或命令继续使用。"

func (a *App) Shutdown(_ context.Context) error {
	a.shutdownMu.Lock()
	if a.shutdownStarted {
		a.shutdownMu.Unlock()
		return nil
	}
	a.shutdownStarted = true
	a.shutdownMu.Unlock()

	events := a.beginShutdownNotices()

	a.stopIngressPump()
	if a.relay != nil {
		_ = a.relay.Close()
	}
	if a.relayServer != nil {
		_ = a.relayServer.Close()
	}
	if a.apiServer != nil {
		_ = a.apiServer.Close()
	}
	a.clearListeners()

	a.deliverShutdownNotices(events)
	a.stopGatewayRuntime()
	cleanupErr := a.shutdownManagedHeadless()

	if a.rawLogger != nil {
		_ = a.rawLogger.Close()
	}
	return cleanupErr
}

func (a *App) setGatewayRuntime(cancel context.CancelFunc, done chan struct{}) {
	a.shutdownMu.Lock()
	defer a.shutdownMu.Unlock()
	a.gatewayRunCancel = cancel
	a.gatewayRunDone = done
}

func (a *App) stopGatewayRuntime() {
	a.shutdownMu.Lock()
	cancel := a.gatewayRunCancel
	done := a.gatewayRunDone
	a.gatewayRunCancel = nil
	a.gatewayRunDone = nil
	a.shutdownMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done == nil {
		return
	}

	timer := time.NewTimer(a.gatewayStopTimeoutValue())
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		log.Printf("daemon shutdown: gateway stop exceeded timeout=%s", a.gatewayStopTimeoutValue())
	}
}

func (a *App) beginShutdownNotices() []control.UIEvent {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.shuttingDown = true
	surfaces := a.service.Surfaces()
	events := make([]control.UIEvent, 0, len(surfaces))
	seen := make(map[string]struct{}, len(surfaces))
	for _, surface := range surfaces {
		if surface == nil {
			continue
		}
		surfaceID := strings.TrimSpace(surface.SurfaceSessionID)
		if surfaceID == "" {
			continue
		}
		if _, ok := seen[surfaceID]; ok {
			continue
		}
		seen[surfaceID] = struct{}{}
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surfaceID,
			Notice: &control.Notice{
				Code: "daemon_shutting_down",
				Text: daemonShutdownNoticeText,
			},
		})
	}
	return events
}

func (a *App) deliverShutdownNotices(events []control.UIEvent) {
	if len(events) == 0 {
		return
	}

	deadline := time.Now().Add(a.shutdownGracePeriodValue())
	for _, event := range events {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			log.Printf("daemon shutdown: final notice grace exhausted before surface=%s", event.SurfaceSessionID)
			return
		}
		timeout := remaining
		if perNotice := a.shutdownNoticeTimeoutValue(); perNotice > 0 && perNotice < timeout {
			timeout = perNotice
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		err := a.deliverUIEventWithContext(ctx, event)
		cancel()
		if err != nil {
			log.Printf("daemon shutdown: final notice failed: surface=%s err=%v", event.SurfaceSessionID, err)
		}
	}
}

func (a *App) clearListeners() {
	a.listenMu.Lock()
	defer a.listenMu.Unlock()
	a.relayListener = nil
	a.apiListener = nil
}

func (a *App) shutdownGracePeriodValue() time.Duration {
	if a.shutdownGracePeriod <= 0 {
		return 5 * time.Second
	}
	return a.shutdownGracePeriod
}

func (a *App) shutdownNoticeTimeoutValue() time.Duration {
	if a.shutdownNoticeTimeout <= 0 {
		return 2 * time.Second
	}
	return a.shutdownNoticeTimeout
}

func (a *App) gatewayStopTimeoutValue() time.Duration {
	if a.gatewayStopTimeout <= 0 {
		return 3 * time.Second
	}
	return a.gatewayStopTimeout
}
