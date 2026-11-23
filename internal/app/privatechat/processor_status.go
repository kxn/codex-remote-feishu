package privatechat

import (
	"context"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// RuntimeStatus is a detached observation, never another mutable core. Host
// readers obtain it before taking their own configuration locks.
type RuntimeStatus struct {
	Instances       []*state.InstanceRecord
	Surfaces        []*state.SurfaceConsoleRecord
	Snapshots       map[string]*control.Snapshot
	Pending, Active []orchestrator.RemoteTurnStatus
}

func (p *Processor) ReadRuntimeStatus(ctx context.Context) (RuntimeStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return RuntimeStatus{}, err
	}
	if p.poisoned {
		return RuntimeStatus{}, ErrProcessorPoisoned
	}
	s := p.app.Service()
	value := RuntimeStatus{Instances: s.Instances(), Surfaces: s.Surfaces(), Pending: s.PendingRemoteTurns(), Active: s.ActiveRemoteTurns(), Snapshots: make(map[string]*control.Snapshot)}
	for _, surface := range value.Surfaces {
		if surface != nil {
			value.Snapshots[surface.SurfaceSessionID] = s.SurfaceSnapshot(surface.SurfaceSessionID)
		}
	}
	// Public core query methods return owned record pointers. Deep-copy all
	// nested queues, thread maps and policy pointers before releasing the lock.
	raw, err := json.Marshal(value)
	if err != nil {
		return RuntimeStatus{}, err
	}
	var detached RuntimeStatus
	err = json.Unmarshal(raw, &detached)
	return detached, err
}
