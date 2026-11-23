package wrapper

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"io"
	"os"
	"os/exec"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
)

func (a *App) managedInspectionEnabled() bool {
	if a.work != nil {
		return a.work.journal != nil && a.work.snapshot.InstanceGeneration > 0 && a.work.snapshot.ProcessGeneration > 0 && a.work.snapshot.LocationGeneration >= 0
	}
	return a.privateNative != nil && a.privateNative.journal != nil && a.privateNative.snapshot.Owner.InstanceGeneration > 0 && a.privateNative.snapshot.Owner.ProcessGeneration > 0
}
func (a *App) inspectionState() *workInspectionState {
	if a.work != nil {
		return a.work.inspection
	}
	if a.privateNative != nil {
		return a.privateNative.inspection
	}
	return nil
}
func (a *App) persistInspection() error {
	if a.work != nil {
		return a.work.inspection.persist(a.work.journal, a.work.snapshot.Owner)
	}
	if a.privateNative != nil {
		return a.privateNative.inspection.persistPrivate(a.privateNative.journal, a.privateNative.snapshot.Owner)
	}
	return nil
}
func (a *App) prepareWorkProcess(cmd *exec.Cmd) {
	if a.managedInspectionEnabled() {
		hostinventory.Prepare(cmd)
	}
}
func (a *App) bindWorkProcess(cmd *exec.Cmd) (*hostinventory.Scope, error) {
	if !a.managedInspectionEnabled() {
		return nil, nil
	}
	s := a.inspectionState()
	scope, err := hostinventory.Bind(cmd)
	if err != nil {
		s.mu.Lock()
		s.startupProcessUntracked = true
		if a.privateNative != nil {
			s.uncertainties["startup_process_untracked"] = true
			s.dirty = true
		}
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Lock()
	s.scopes = append(s.scopes, scope)
	s.dirty = true
	s.nativeReady = false
	identity := scope.Inspect()
	s.scopeID = fmt.Sprintf("%d:%s", identity.RootPID, identity.RootIdentity)
	s.cliSession = ""
	s.mu.Unlock()
	if err = a.persistInspection(); err != nil {
		return scope, err
	}
	return scope, nil
}

func (a *App) carrierProcessIdentity() *agentproto.RuntimeProcessIdentity {
	if a.work == nil && a.privateNative == nil {
		return nil
	}
	identity, err := hostinventory.CaptureProcess(os.Getpid())
	if err != nil {
		return nil
	}
	hostID := ""
	if a.work != nil {
		hostID = a.work.snapshot.HostID
	} else {
		hostID = a.privateNative.snapshot.Owner.HostID
	}
	return &agentproto.RuntimeProcessIdentity{HostID: hostID, Platform: identity.Platform, PID: identity.PID, StartIdentity: identity.StartIdentity}
}

func abortNativeStartup(cmd *exec.Cmd, cancel context.CancelFunc, scope *hostinventory.Scope, pipes ...io.Closer) {
	scope.MarkControlledStop()
	cancel()
	_ = cmd.Wait()
	scope.MarkWaitObserved()
	for _, pipe := range pipes {
		_ = pipe.Close()
	}
}
