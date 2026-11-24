package orchestrator

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// ApplyPrivateTransportDisconnected records connectivity only. It never
// declares a native turn stopped or consumes/clears any queued input.
func (s *Service) ApplyPrivateTransportDisconnected(surfaceID, instanceID string) ([]eventcontract.Event, error) {
	if !s.config.PrivateApplication {
		return nil, fmt.Errorf("private service required")
	}
	surface := s.root.Surfaces[surfaceID]
	instance := s.root.Instances[instanceID]
	if surface == nil || instance == nil {
		return nil, fmt.Errorf("private disconnect owner mismatch")
	}
	if surface.AttachedInstanceID != instanceID && (surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != instanceID) {
		instance.Online = false
		return nil, nil
	}
	checkpoint, err := s.ExportPrivateCheckpoint(surfaceID)
	if err != nil {
		return nil, err
	}
	instance.Online = false
	if len(checkpoint.Holds) > 0 || checkpoint.ActiveQueueItemID != "" || len(checkpoint.QueuedQueueItemIDs) > 0 {
		if _, held := s.privateRecoveryHolds[surfaceID]; !held {
			s.privateRecoveryHolds[surfaceID] = PrivateCheckpoint{Version: 1, SurfaceID: surfaceID}
		}
	}
	return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: surface.GatewayID, SurfaceSessionID: surfaceID, Notice: &control.Notice{Code: "private_transport_offline", Title: "连接暂时中断", Text: "运行环境连接已中断，正在恢复连接并核对未完成的操作。"}}}, nil
}

// ReconcilePrivateRecoveryGate runs after a verified durable recovery fact.
// It releases the gate only when all other unresolved state has cleared.
func (s *Service) ReconcilePrivateRecoveryGate(surfaceID string, queueProven bool) ([]eventcontract.Event, error) {
	if !s.config.PrivateApplication {
		return nil, fmt.Errorf("private service required")
	}
	held, ok := s.privateRecoveryHolds[surfaceID]
	if !ok {
		return nil, nil
	}
	checkpoint, err := s.ExportPrivateCheckpoint(surfaceID)
	if err != nil {
		return nil, err
	}
	if len(checkpoint.Holds) != 0 || checkpoint.ActiveQueueItemID != "" || checkpoint.AutoWhip.PendingReason != "" || checkpoint.AutoContinue.Episode != nil || len(checkpoint.QueuedQueueItemIDs) != 0 && !queueProven && !held.MigrationRecoveryReleased {
		return nil, nil
	}
	delete(s.privateRecoveryHolds, surfaceID)
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return nil, fmt.Errorf("private surface missing")
	}
	s.restoreSurfaceDispatchNormal(surface)
	return s.dispatchNext(surface), nil
}

// MarkPrivateStopProjectionOffline prevents dispatch to the carrier being retired
// without changing the user’s dispatch preference.
func (s *Service) MarkPrivateStopProjectionOffline(surfaceID, instanceID string) error {
	v := s.root.Surfaces[surfaceID]
	if !s.config.PrivateApplication || v == nil || v.AttachedInstanceID != instanceID {
		return fmt.Errorf("private stop projection owner mismatch")
	}
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return fmt.Errorf("private stopped instance missing")
	}
	inst.Online = false
	return nil
}

// MaterializePrivateStoppedInstance restores only offline carrier metadata so
// authenticated persisted terminal facts can be projected after a restart.
func (s *Service) MaterializePrivateStoppedInstance(surfaceID string, owner agentproto.NativeOwner) error {
	v := s.root.Surfaces[surfaceID]
	if !s.config.PrivateApplication || v == nil || v.AttachedInstanceID != owner.InstanceID || owner.Validate() != nil {
		return fmt.Errorf("private stopped owner mismatch")
	}
	if inst := s.root.Instances[owner.InstanceID]; inst != nil {
		return nil
	}
	cp, err := s.ExportPrivateCheckpoint(surfaceID)
	if err != nil {
		return err
	}
	h := cp.HistoricalInstance
	if h == nil || h.InstanceID != owner.InstanceID || h.Backend != owner.Backend || h.WorkspaceRoot == "" {
		return fmt.Errorf("private stopped instance metadata missing")
	}
	if cp.Navigation != nil && cp.Navigation.Owner != owner {
		return fmt.Errorf("private stopped carrier identity mismatch")
	}
	s.UpsertInstance(&state.InstanceRecord{InstanceID: owner.InstanceID, Source: "headless", Managed: true, Online: false, Backend: owner.Backend, WorkspaceRoot: h.WorkspaceRoot, WorkspaceKey: h.WorkspaceKey, Threads: map[string]*state.ThreadRecord{}})
	return nil
}
