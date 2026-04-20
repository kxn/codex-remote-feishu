package daemon

import (
	"context"
	"net"
	"net/http"
	"time"
)

type headlessRestoreRecoveryState struct {
	Entry           SurfaceResumeEntry
	NextAttemptAt   time.Time
	LastAttemptAt   time.Time
	LastFailureCode string
}

type surfaceResumeRecoveryState struct {
	Entry           SurfaceResumeEntry
	NextAttemptAt   time.Time
	LastAttemptAt   time.Time
	LastFailureCode string
}

type vscodeMigrationFlowRecord struct {
	FlowID           string
	SurfaceSessionID string
	OwnerUserID      string
	MessageID        string
	IssueKey         string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ExpiresAt        time.Time
}

type toolRuntimeState struct {
	server      *http.Server
	listener    net.Listener
	statePath   string
	bearerToken string
}

type surfaceResumeRuntimeState struct {
	store                  *surfaceResumeStore
	recovery               map[string]*surfaceResumeRecoveryState
	vscodeMigrationFlows   map[string]*vscodeMigrationFlowRecord
	vscodeMigrationNextSeq int64
	vscodeResumeNotices    map[string]bool
	vscodeStartupCheckDue  bool
	headlessRestore        map[string]*headlessRestoreRecoveryState
	startupRefreshPending  map[string]bool
	startupRefreshSeen     bool
	workspaceContextRoots  map[string]string
}

type upgradeRuntimeState struct {
	lookup          releaseLookupFunc
	devManifest     devManifestLookupFunc
	checkInterval   time.Duration
	startupDelay    time.Duration
	promptScanEvery time.Duration
	resultScanEvery time.Duration
	checkInFlight   bool
	startInFlight   bool
	nextCheckAt     time.Time
	nextPromptScan  time.Time
	nextResultScan  time.Time
	nextFlowSeq     int64
	activeFlow      *upgradeOwnerCardFlowRecord
	startCancel     context.CancelFunc
	startFlowID     string
}

func newSurfaceResumeRuntimeState() surfaceResumeRuntimeState {
	return surfaceResumeRuntimeState{
		recovery:              map[string]*surfaceResumeRecoveryState{},
		vscodeMigrationFlows:  map[string]*vscodeMigrationFlowRecord{},
		vscodeResumeNotices:   map[string]bool{},
		headlessRestore:       map[string]*headlessRestoreRecoveryState{},
		startupRefreshPending: map[string]bool{},
		workspaceContextRoots: map[string]string{},
	}
}

func newUpgradeRuntimeState() upgradeRuntimeState {
	return upgradeRuntimeState{
		checkInterval:   3 * time.Hour,
		startupDelay:    1 * time.Minute,
		promptScanEvery: 5 * time.Second,
		resultScanEvery: 5 * time.Second,
	}
}
