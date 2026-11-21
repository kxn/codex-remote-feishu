package wrapper

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"os"
	"os/exec"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
)

func (a *App) managedInspectionEnabled() bool {
	return a.work != nil && a.work.journal != nil && a.work.snapshot.InstanceGeneration > 0 && a.work.snapshot.ProcessGeneration > 0 && a.work.snapshot.LocationGeneration >= 0
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
	scope, err := hostinventory.Bind(cmd)
	if err != nil {
		return nil, err
	}
	a.work.inspection.mu.Lock()
	a.work.inspection.scopes = append(a.work.inspection.scopes, scope)
	a.work.inspection.dirty = true
	a.work.inspection.nativeReady = false
	identity := scope.Inspect()
	a.work.inspection.scopeID = fmt.Sprintf("%d:%s", identity.RootPID, identity.RootIdentity)
	a.work.inspection.cliSession = ""
	a.work.inspection.mu.Unlock()
	if err := a.work.inspection.persist(a.work.journal, a.work.snapshot.Owner); err != nil {
		return nil, err
	}
	return scope, nil
}

func (a *App) carrierProcessIdentity() *agentproto.RuntimeProcessIdentity {
	if a.work == nil {
		return nil
	}
	identity, err := hostinventory.CaptureProcess(os.Getpid())
	if err != nil {
		return nil
	}
	return &agentproto.RuntimeProcessIdentity{HostID: a.work.snapshot.HostID, Platform: identity.Platform, PID: identity.PID, StartIdentity: identity.StartIdentity}
}
