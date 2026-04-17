package daemon

import (
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

type toolRuntimeState struct {
	server      *http.Server
	listener    net.Listener
	statePath   string
	bearerToken string
}

type surfaceResumeRuntimeState struct {
	store                  *surfaceResumeStore
	recovery               map[string]*surfaceResumeRecoveryState
	vscodeResumeNotices    map[string]bool
	vscodeMigrationPrompts map[string]string
	vscodeStartupCheckDue  bool
	headlessRestore        map[string]*headlessRestoreRecoveryState
	startupRefreshPending  map[string]bool
	startupRefreshSeen     bool
	workspaceContextRoots  map[string]string
}

func newSurfaceResumeRuntimeState() surfaceResumeRuntimeState {
	return surfaceResumeRuntimeState{
		recovery:               map[string]*surfaceResumeRecoveryState{},
		vscodeResumeNotices:    map[string]bool{},
		vscodeMigrationPrompts: map[string]string{},
		headlessRestore:        map[string]*headlessRestoreRecoveryState{},
		startupRefreshPending:  map[string]bool{},
		workspaceContextRoots:  map[string]string{},
	}
}
