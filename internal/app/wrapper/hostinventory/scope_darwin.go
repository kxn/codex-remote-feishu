//go:build darwin

package hostinventory

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

type darwinScope struct{ group int }

func preparePlatform(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
func darwinIdentity(info *unix.KinfoProc) string {
	return fmt.Sprintf("%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec)
}
func bindPlatform(cmd *exec.Cmd) (*Scope, error) {
	if cmd.Process == nil {
		return nil, fmt.Errorf("native process not started")
	}
	pid := cmd.Process.Pid
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return nil, err
	}
	if int(info.Eproc.Pgid) != pid {
		return nil, fmt.Errorf("native process group is not isolated")
	}
	return &Scope{pid: pid, identity: darwinIdentity(info), platform: &darwinScope{group: pid}}, nil
}
func (p *darwinScope) Inspect(scope *Scope) Snapshot {
	result := scope.base()
	result.Platform = "darwin"
	result.GroupID = p.group
	info, err := unix.SysctlKinfoProc("kern.proc.pid", scope.pid)
	if err == nil && info != nil && darwinIdentity(info) != scope.identity {
		result.Reason = "native_root_pid_reused"
		return result
	}
	result.RootAlive = err == nil && info != nil && int(info.Proc.P_pid) == scope.pid && darwinIdentity(info) == scope.identity && int(info.Eproc.Pgid) == p.group && info.Proc.P_stat != 5
	if !result.RootAlive && !(result.RootExitVerified || (result.ControlledStop && result.WaitObserved)) {
		result.Reason = "native_root_identity_missing_or_changed"
		return result
	}
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		result.Reason = "host_process_inventory_unavailable"
		return result
	}
	for _, info := range processes {
		pid := int(info.Proc.P_pid)
		if pid != scope.pid && int(info.Eproc.Pgid) == p.group && info.Proc.P_stat != 5 {
			result.ProcessIDs = append(result.ProcessIDs, pid)
		}
	}
	if result.RootAlive {
		again, err := unix.SysctlKinfoProc("kern.proc.pid", scope.pid)
		if err != nil || again == nil || darwinIdentity(again) != scope.identity {
			result.Reason = "native_root_changed_during_inventory"
			return result
		}
	}
	result.Complete = true
	return result
}
func (p *darwinScope) StopChildren(ctx context.Context, scope *Scope) error {
	snapshot := p.Inspect(scope)
	if !snapshot.Complete {
		return fmt.Errorf("native process group is not verified")
	}
	for _, pid := range snapshot.ProcessIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err != nil {
			continue
		}
		if int(info.Eproc.Pgid) != p.group {
			continue
		}
		if err := unix.Kill(pid, unix.SIGTERM); err != nil && err != unix.ESRCH {
			return err
		}
	}
	return nil
}
func (p *darwinScope) Close() error { return nil }

func restorePlatform(snapshot Snapshot) (*Scope, error) {
	if snapshot.Platform != "darwin" || snapshot.RootPID < 1 || snapshot.GroupID != snapshot.RootPID || snapshot.RootIdentity == "" {
		return nil, fmt.Errorf("invalid durable native macOS scope")
	}
	return &Scope{pid: snapshot.RootPID, identity: snapshot.RootIdentity, platform: &darwinScope{group: snapshot.GroupID}}, nil
}
