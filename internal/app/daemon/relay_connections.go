package daemon

import (
	"time"
)

const overloadNoticeWindow = 5 * time.Second

type relayConnectionState struct {
	currentConnectionID  uint64
	degradedConnectionID uint64
	lastOverloadNoticeAt time.Time
}

func (a *App) rememberRelayConnection(instanceID string, connectionID uint64) {
	if instanceID == "" || connectionID == 0 {
		return
	}
	a.relayConnMu.Lock()
	defer a.relayConnMu.Unlock()
	state := a.ensureRelayConnectionState(instanceID)
	state.currentConnectionID = connectionID
}

func (a *App) currentRelayConnection(instanceID string) uint64 {
	a.relayConnMu.Lock()
	defer a.relayConnMu.Unlock()
	state := a.relayConnections[instanceID]
	if state == nil {
		return 0
	}
	return state.currentConnectionID
}

func (a *App) markRelayConnectionDropped(instanceID string, connectionID uint64) (current bool, degraded bool) {
	a.relayConnMu.Lock()
	defer a.relayConnMu.Unlock()

	state := a.relayConnections[instanceID]
	if state == nil {
		return false, false
	}
	if state.currentConnectionID == connectionID {
		current = true
		state.currentConnectionID = 0
	}
	if state.degradedConnectionID == connectionID {
		degraded = true
		state.degradedConnectionID = 0
	}
	if state.currentConnectionID == 0 && state.degradedConnectionID == 0 {
		delete(a.relayConnections, instanceID)
	}
	return current, degraded
}

func (a *App) beginRelayTransportDegrade(instanceID string, connectionID uint64, now time.Time) (apply bool, emitNotice bool) {
	if instanceID == "" || connectionID == 0 {
		return false, false
	}
	a.relayConnMu.Lock()
	defer a.relayConnMu.Unlock()

	state := a.ensureRelayConnectionState(instanceID)
	if state.currentConnectionID != 0 && state.currentConnectionID != connectionID {
		return false, false
	}
	if state.degradedConnectionID == connectionID {
		return false, false
	}
	state.degradedConnectionID = connectionID
	if now.IsZero() {
		now = time.Now()
	}
	if state.lastOverloadNoticeAt.IsZero() || now.Sub(state.lastOverloadNoticeAt) >= overloadNoticeWindow {
		state.lastOverloadNoticeAt = now
		emitNotice = true
	}
	return true, emitNotice
}

func (a *App) ensureRelayConnectionState(instanceID string) *relayConnectionState {
	state := a.relayConnections[instanceID]
	if state == nil {
		state = &relayConnectionState{}
		a.relayConnections[instanceID] = state
	}
	return state
}
