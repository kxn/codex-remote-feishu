package daemon

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (a *App) applicationRuntimeStatus(ctx context.Context) runtimeStatusPayload {
	payload := runtimeStatusPayload{Authority: "separate_applications", Instances: []*state.InstanceRecord{}, Surfaces: []*state.SurfaceConsoleRecord{}, PendingRemoteTurns: []orchestrator.RemoteTurnStatus{}, ActiveRemoteTurns: []orchestrator.RemoteTurnStatus{}, Gateways: gatewayStatuses(a.gateway), Work: &runtimeWorkStatus{}}
	private, err := a.applications.processor.ReadRuntimeStatus(ctx)
	if err != nil {
		payload.PrivateReadError = err.Error()
	} else {
		payload.Instances, payload.Surfaces = private.Instances, private.Surfaces
		payload.PendingRemoteTurns, payload.ActiveRemoteTurns = private.Pending, private.Active
		payload.SurfaceStatuses = runtimeSurfaceStatuses(private.Surfaces, func(id string) *control.Snapshot { return private.Snapshots[id] })
	}
	if a.teamRuntime == nil || a.teamRuntime.store == nil {
		payload.Work.ReadError = "team work store is unavailable"
		return payload
	}
	team, err := a.teamRuntime.store.ReadRuntimeStatus(ctx)
	if err != nil {
		payload.Work.ReadError = err.Error()
		return payload
	}
	payload.Work.Available = true
	payload.Work.RuntimeStatus = &team
	return payload
}
