package orchestrator

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// ApplyPrivateTransportDisconnected records connectivity only. It never
// declares a native turn stopped or consumes/clears any queued input.
func (s *Service) ApplyPrivateTransportDisconnected(surfaceID, instanceID string) ([]eventcontract.Event, error) {
	if !s.config.PrivateApplication {
		return nil, fmt.Errorf("private service required")
	}
	surface := s.root.Surfaces[surfaceID]
	instance := s.root.Instances[instanceID]
	if surface == nil || instance == nil || (surface.AttachedInstanceID != instanceID && (surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != instanceID)) {
		return nil, fmt.Errorf("private disconnect owner mismatch")
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
