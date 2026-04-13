package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/shutdownctx"
)

const daemonShutdownNoticeText = "daemon 正在关闭，当前飞书窗口会暂时离线。若稍后完成重启或升级，请重新发送消息或命令继续使用。"

type relayShutdownTarget struct {
	InstanceID string
	PID        int
}

type relayShutdownObservedInstance struct {
	PID int
}

func (a *App) Shutdown(ctx context.Context) error {
	a.shutdownMu.Lock()
	if a.shutdownStarted {
		a.shutdownMu.Unlock()
		return nil
	}
	a.shutdownStarted = true
	a.shutdownMu.Unlock()

	if shutdownMode(ctx) == shutdownctx.ModeConsoleClose {
		return a.shutdownForConsoleClose()
	}

	events := a.beginShutdownNotices()
	handledRelayTargets, relayDrainErr := a.shutdownRelayInstancesWithTimeout(a.shutdownDrainTimeoutValue(), a.shutdownDrainPollValue())
	a.stopIngressAndServers()

	a.deliverShutdownNotices(events)
	a.stopGatewayRuntime(true)
	cleanupErr := a.shutdownManagedHeadless(handledRelayTargets)
	return a.finishShutdown(relayDrainErr, cleanupErr)
}

func shutdownMode(ctx context.Context) shutdownctx.Mode {
	return shutdownctx.ModeFrom(ctx)
}

func (a *App) setGatewayRuntime(cancel context.CancelFunc, done chan struct{}) {
	a.shutdownMu.Lock()
	defer a.shutdownMu.Unlock()
	a.gatewayRunCancel = cancel
	a.gatewayRunDone = done
}

func (a *App) shutdownForConsoleClose() error {
	a.mu.Lock()
	a.shuttingDown = true
	a.mu.Unlock()
	handledRelayTargets, relayDrainErr := a.shutdownRelayInstancesWithTimeout(250*time.Millisecond, 25*time.Millisecond)
	cleanupErr := a.shutdownManagedHeadless(handledRelayTargets)
	a.stopIngressAndServers()
	a.stopGatewayRuntime(false)
	return a.finishShutdown(relayDrainErr, cleanupErr)
}

func (a *App) stopGatewayRuntime(wait bool) {
	a.shutdownMu.Lock()
	cancel := a.gatewayRunCancel
	done := a.gatewayRunDone
	a.gatewayRunCancel = nil
	a.gatewayRunDone = nil
	a.shutdownMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done == nil || !wait {
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
	a.pprofListener = nil
	a.toolListener = nil
	a.externalAccessListener = nil
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

func (a *App) shutdownDrainTimeoutValue() time.Duration {
	if a.shutdownDrainTimeout <= 0 {
		return 3 * time.Second
	}
	return a.shutdownDrainTimeout
}

func (a *App) shutdownDrainPollValue() time.Duration {
	if a.shutdownDrainPoll <= 0 {
		return 50 * time.Millisecond
	}
	return a.shutdownDrainPoll
}

func (a *App) shutdownRelayInstances() (map[string]struct{}, error) {
	return a.shutdownRelayInstancesWithTimeout(a.shutdownDrainTimeoutValue(), a.shutdownDrainPollValue())
}

func (a *App) shutdownRelayInstancesWithTimeout(drainTimeout, poll time.Duration) (map[string]struct{}, error) {
	targets := a.collectRelayShutdownTargets()
	if len(targets) == 0 {
		return nil, nil
	}

	var errs []error
	for _, target := range targets {
		command := agentproto.Command{
			CommandID: a.nextCommandID(),
			Kind:      agentproto.CommandProcessExit,
		}
		if err := a.sendAgentCommand(target.InstanceID, command); err != nil {
			if a.currentRelayConnection(target.InstanceID) == 0 {
				continue
			}
			log.Printf("daemon shutdown: relay exit command failed: instance=%s pid=%d err=%v", target.InstanceID, target.PID, err)
			errs = append(errs, fmt.Errorf("send process.exit to %s: %w", target.InstanceID, err))
			continue
		}
		log.Printf("daemon shutdown: requested wrapper exit: instance=%s pid=%d", target.InstanceID, target.PID)
	}

	remaining := a.waitForRelayShutdownTargetsWithTimeout(targets, drainTimeout, poll)
	handled := make(map[string]struct{}, len(targets))
	remainingSet := make(map[string]relayShutdownTarget, len(remaining))
	for _, target := range remaining {
		remainingSet[target.InstanceID] = target
	}
	for _, target := range targets {
		if _, ok := remainingSet[target.InstanceID]; !ok {
			handled[target.InstanceID] = struct{}{}
		}
	}

	for _, target := range remaining {
		if target.PID <= 0 {
			log.Printf("daemon shutdown: wrapper exit timed out with unknown pid: instance=%s", target.InstanceID)
			continue
		}
		if err := a.stopProcess(target.PID, a.shutdownForceKillGrace); err != nil {
			log.Printf("daemon shutdown: force stop wrapper failed: instance=%s pid=%d err=%v", target.InstanceID, target.PID, err)
			errs = append(errs, fmt.Errorf("force stop %s (pid %d): %w", target.InstanceID, target.PID, err))
			continue
		}
		handled[target.InstanceID] = struct{}{}
		log.Printf("daemon shutdown: force-stopped wrapper after timeout: instance=%s pid=%d", target.InstanceID, target.PID)
	}

	return handled, errors.Join(errs...)
}

func (a *App) collectRelayShutdownTargets() []relayShutdownTarget {
	connections := a.snapshotRelayConnections()
	if len(connections) == 0 {
		return nil
	}
	instances := a.snapshotRelayInstancesForShutdown()
	targets := make([]relayShutdownTarget, 0, len(connections))
	for instanceID, connection := range connections {
		if connection.CurrentConnectionID == 0 {
			continue
		}
		target := relayShutdownTarget{
			InstanceID: strings.TrimSpace(instanceID),
			PID:        connection.PID,
		}
		if observed, ok := instances[instanceID]; ok {
			if observed.PID > 0 {
				target.PID = observed.PID
			}
		}
		if target.InstanceID == "" {
			continue
		}
		targets = append(targets, target)
	}
	return targets
}

func (a *App) snapshotRelayInstancesForShutdown() map[string]relayShutdownObservedInstance {
	a.mu.Lock()
	defer a.mu.Unlock()

	snapshot := make(map[string]relayShutdownObservedInstance)
	for _, inst := range a.service.Instances() {
		if inst == nil || strings.TrimSpace(inst.InstanceID) == "" {
			continue
		}
		snapshot[inst.InstanceID] = relayShutdownObservedInstance{
			PID: inst.PID,
		}
	}
	return snapshot
}

func (a *App) waitForRelayShutdownTargets(targets []relayShutdownTarget) []relayShutdownTarget {
	return a.waitForRelayShutdownTargetsWithTimeout(targets, a.shutdownDrainTimeoutValue(), a.shutdownDrainPollValue())
}

func (a *App) waitForRelayShutdownTargetsWithTimeout(targets []relayShutdownTarget, drainTimeout, poll time.Duration) []relayShutdownTarget {
	if len(targets) == 0 {
		return nil
	}
	if drainTimeout <= 0 {
		return a.remainingRelayShutdownTargets(targets)
	}
	deadline := time.Now().Add(drainTimeout)
	for {
		remaining := a.remainingRelayShutdownTargets(targets)
		if len(remaining) == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return remaining
		}
		sleepFor := poll
		if sleepFor <= 0 {
			sleepFor = 10 * time.Millisecond
		}
		if remainingBudget := time.Until(deadline); remainingBudget < sleepFor {
			sleepFor = remainingBudget
		}
		if sleepFor <= 0 {
			return remaining
		}
		time.Sleep(sleepFor)
	}
}

func (a *App) stopIngressAndServers() {
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
	if a.toolServer != nil {
		_ = a.toolServer.Close()
	}
	if a.pprofServer != nil {
		_ = a.pprofServer.Close()
	}
	a.mu.Lock()
	a.removeToolServiceStateLocked()
	a.clearWorkspaceSurfaceContextFilesLocked()
	a.shutdownExternalAccessLocked("daemon_shutdown")
	a.mu.Unlock()
	a.clearListeners()
}

func (a *App) finishShutdown(errs ...error) error {
	if a.rawLogger != nil {
		_ = a.rawLogger.Close()
	}
	if a.conversationTrace != nil {
		_ = a.conversationTrace.Close()
	}
	return errors.Join(errs...)
}

func (a *App) remainingRelayShutdownTargets(targets []relayShutdownTarget) []relayShutdownTarget {
	remaining := make([]relayShutdownTarget, 0, len(targets))
	for _, target := range targets {
		if a.currentRelayConnection(target.InstanceID) == 0 {
			continue
		}
		remaining = append(remaining, target)
	}
	return remaining
}
