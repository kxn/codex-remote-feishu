package daemon

import (
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
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

type cronRuntimeState struct {
	stateIOMu             sync.Mutex
	loaded                bool
	syncInFlight          bool
	state                 *cronStateFile
	runs                  map[string]*cronRunState
	jobActiveRuns         map[string]map[string]struct{}
	exitTargets           map[string]*cronExitTarget
	bitableFactory        func(string) (feishu.BitableAPI, error)
	gatewayIdentityLookup func(string) (cronGatewayIdentity, bool, error)
	nextScheduleScan      time.Time
	repoManager           *cronrepo.Manager
}

type feishuRuntimeState struct {
	mu                        sync.RWMutex
	permissionMu              sync.RWMutex
	runtimeApply              map[string]feishuRuntimeApplyPendingState
	timeSensitive             map[string]feishuTimeSensitiveState
	attentionRequests         map[string]time.Time
	permissionGaps            map[string]map[string]*feishuPermissionGapRecord
	permissionRefreshEvery    time.Duration
	permissionNextRefresh     time.Time
	permissionRefreshInFlight bool
	onboarding                map[string]*feishuOnboardingSession
	setup                     feishuSetupClient
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

func newCronRuntimeState() cronRuntimeState {
	return cronRuntimeState{
		runs:          map[string]*cronRunState{},
		jobActiveRuns: map[string]map[string]struct{}{},
		exitTargets:   map[string]*cronExitTarget{},
	}
}

func newFeishuRuntimeState() feishuRuntimeState {
	return feishuRuntimeState{
		runtimeApply:           map[string]feishuRuntimeApplyPendingState{},
		timeSensitive:          map[string]feishuTimeSensitiveState{},
		attentionRequests:      map[string]time.Time{},
		permissionGaps:         map[string]map[string]*feishuPermissionGapRecord{},
		permissionRefreshEvery: defaultFeishuPermissionRefreshEvery,
		onboarding:             map[string]*feishuOnboardingSession{},
	}
}
