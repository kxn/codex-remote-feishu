package daemon

import (
	upgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/upgraderuntime"
)

type releaseLookupFunc = upgraderuntime.ReleaseLookupFunc
type devManifestLookupFunc = upgraderuntime.DevManifestLookupFunc
type upgradeOwnerCardFlowStage = upgraderuntime.OwnerCardFlowStage
type upgradeOwnerCardFlowRecord = upgraderuntime.OwnerCardFlowRecord
type upgradeRuntimeState = upgraderuntime.State

const (
	upgradeOwnerFlowStageChecking   = upgraderuntime.OwnerFlowStageChecking
	upgradeOwnerFlowStageConfirm    = upgraderuntime.OwnerFlowStageConfirm
	upgradeOwnerFlowStageRunning    = upgraderuntime.OwnerFlowStageRunning
	upgradeOwnerFlowStageCancelling = upgraderuntime.OwnerFlowStageCancelling
	upgradeOwnerFlowStageRestarting = upgraderuntime.OwnerFlowStageRestarting
	upgradeOwnerFlowStageCompleted  = upgraderuntime.OwnerFlowStageCompleted
	upgradeOwnerFlowStageCancelled  = upgraderuntime.OwnerFlowStageCancelled
	upgradeOwnerFlowStageFailed     = upgraderuntime.OwnerFlowStageFailed
)

func newUpgradeRuntimeState() upgradeRuntimeState {
	return upgraderuntime.NewState()
}
