package upgraderuntime

import (
	"context"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
)

type ReleaseLookupFunc func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error)

type DevManifestLookupFunc func(context.Context) (install.DevManifest, install.DevManifestAsset, error)

type OwnerCardFlowStage string

const (
	OwnerFlowStageChecking   OwnerCardFlowStage = "checking"
	OwnerFlowStageConfirm    OwnerCardFlowStage = "confirm"
	OwnerFlowStageRunning    OwnerCardFlowStage = "running"
	OwnerFlowStageCancelling OwnerCardFlowStage = "cancelling"
	OwnerFlowStageRestarting OwnerCardFlowStage = "restarting"
	OwnerFlowStageCompleted  OwnerCardFlowStage = "completed"
	OwnerFlowStageCancelled  OwnerCardFlowStage = "cancelled"
	OwnerFlowStageFailed     OwnerCardFlowStage = "failed"
)

type OwnerCardFlowRecord struct {
	FlowID           string
	SurfaceSessionID string
	OwnerUserID      string
	MessageID        string
	Stage            OwnerCardFlowStage
	Source           install.UpgradeSource
	Track            install.ReleaseTrack
	CurrentVersion   string
	TargetVersion    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ExpiresAt        time.Time
	CancelRequested  bool
}

type State struct {
	Lookup          ReleaseLookupFunc
	DevManifest     DevManifestLookupFunc
	CheckInterval   time.Duration
	StartupDelay    time.Duration
	PromptScanEvery time.Duration
	ResultScanEvery time.Duration
	CheckInFlight   bool
	StartInFlight   bool
	NextCheckAt     time.Time
	NextPromptScan  time.Time
	NextResultScan  time.Time
	NextFlowSeq     int64
	ActiveFlow      *OwnerCardFlowRecord
	StartCancel     context.CancelFunc
	StartFlowID     string
}

func NewState() State {
	return State{
		CheckInterval:   3 * time.Hour,
		StartupDelay:    1 * time.Minute,
		PromptScanEvery: 5 * time.Second,
		ResultScanEvery: 5 * time.Second,
	}
}
