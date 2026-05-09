package daemon

import (
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/claudeworkspaceprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
)

type surfaceResumeRecoveryState struct {
	Entry           surfaceresume.Entry
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
	store                  *surfaceresume.Store
	recovery               map[string]*surfaceResumeRecoveryState
	vscodeMigrationFlows   map[string]*vscodeMigrationFlowRecord
	vscodeMigrationNextSeq int64
	vscodeResumeNotices    map[string]bool
	vscodeStartupCheckDue  bool
	startupRefreshPending  map[string]bool
	startupRefreshSeen     bool
	workspaceContextRoots  map[string]string
}

type claudeWorkspaceProfileRuntimeState struct {
	store *claudeworkspaceprofile.Store
}

type cronRuntimeState struct {
	stateIOMu             sync.Mutex
	loaded                bool
	syncInFlight          bool
	state                 *cronrt.StateFile
	runs                  map[string]*cronrt.RunState
	jobActiveRuns         map[string]map[string]struct{}
	exitTargets           map[string]*cronrt.ExitTarget
	bitableFactory        func(string) (feishu.BitableAPI, error)
	gatewayIdentityLookup func(string) (cronrt.GatewayIdentity, bool, error)
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
		startupRefreshPending: map[string]bool{},
		workspaceContextRoots: map[string]string{},
	}
}

func newCronRuntimeState() cronRuntimeState {
	return cronRuntimeState{
		runs:          map[string]*cronrt.RunState{},
		jobActiveRuns: map[string]map[string]struct{}{},
		exitTargets:   map[string]*cronrt.ExitTarget{},
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
