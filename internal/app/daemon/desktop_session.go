package daemon

import (
	"log"
	"os"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/desktopsession"
)

type DesktopSessionRuntimeOptions struct {
	StatePath           string
	InstanceID          string
	BackendPID          int
	AdminURL            string
	SetupURL            string
	SetupRequired       bool
	RequestSelfShutdown func() error
}

type desktopSessionRuntimeState struct {
	statePath           string
	instanceID          string
	backendPID          int
	adminURL            string
	setupURL            string
	setupRequired       bool
	requestSelfShutdown func() error
}

func (a *App) ConfigureDesktopSession(opts DesktopSessionRuntimeOptions) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.desktopSession = desktopSessionRuntimeState{
		statePath:           opts.StatePath,
		instanceID:          opts.InstanceID,
		backendPID:          opts.BackendPID,
		adminURL:            opts.AdminURL,
		setupURL:            opts.SetupURL,
		setupRequired:       opts.SetupRequired,
		requestSelfShutdown: opts.RequestSelfShutdown,
	}
	if a.desktopSession.requestSelfShutdown == nil {
		a.desktopSession.requestSelfShutdown = requestSelfShutdown
	}
}

func (a *App) publishDesktopSessionState(state desktopsession.State) error {
	status, ok := a.desktopSessionStatusSnapshot(state)
	if !ok {
		return nil
	}
	return desktopsession.WriteStatusFile(a.desktopSessionStatePath(), status)
}

func (a *App) clearDesktopSessionState() error {
	return desktopsession.RemoveStatusFile(a.desktopSessionStatePath())
}

func (a *App) desktopSessionStatusPayload() desktopsession.Status {
	state := desktopsession.StateBackendOnly
	a.mu.Lock()
	if a.shuttingDown || a.shutdownStarted {
		state = desktopsession.StateQuitting
	}
	a.mu.Unlock()
	status, ok := a.desktopSessionStatusSnapshot(state)
	if !ok {
		return desktopsession.Status{State: desktopsession.StateNone}
	}
	return status
}

func (a *App) requestDesktopSessionQuit() error {
	a.mu.Lock()
	a.shuttingDown = true
	a.mu.Unlock()
	if err := a.publishDesktopSessionState(desktopsession.StateQuitting); err != nil {
		return err
	}
	trigger := a.desktopSessionRequestSelfShutdown()
	if trigger == nil {
		return nil
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		if err := trigger(); err != nil {
			log.Printf("desktop session quit trigger failed: %v", err)
		}
	}()
	return nil
}

func (a *App) desktopSessionStatusSnapshot(state desktopsession.State) (desktopsession.Status, bool) {
	a.mu.Lock()
	runtimeState := a.desktopSession
	a.mu.Unlock()
	if runtimeState.statePath == "" {
		return desktopsession.Status{}, false
	}
	setupRequired := runtimeState.setupRequired
	if current, ok := a.currentDesktopSessionSetupRequired(); ok {
		setupRequired = current
	}
	return desktopsession.Status{
		State:         state,
		UpdatedAt:     time.Now().UTC(),
		BackendPID:    runtimeState.backendPID,
		InstanceID:    runtimeState.instanceID,
		AdminURL:      runtimeState.adminURL,
		SetupURL:      runtimeState.setupURL,
		SetupRequired: setupRequired,
	}, true
}

func (a *App) desktopSessionStatePath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.desktopSession.statePath
}

func (a *App) desktopSessionRequestSelfShutdown() func() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.desktopSession.requestSelfShutdown
}

func requestSelfShutdown() error {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return process.Signal(os.Interrupt)
}

func (a *App) currentDesktopSessionSetupRequired() (bool, bool) {
	workflow, err := a.buildOnboardingWorkflow("")
	if err == nil {
		return workflow.Completion.SetupRequired, true
	}

	loaded, err := a.loadAdminConfig()
	if err != nil {
		return false, false
	}

	a.mu.Lock()
	admin := a.admin
	binaryPath := a.headlessRuntime.BinaryPath
	a.mu.Unlock()
	return requiresSetup(loaded.Config, admin.services, binaryPath), true
}
