//go:build linux

package hostinventory

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type linuxScope struct{ group int }

func preparePlatform(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
func bindPlatform(cmd *exec.Cmd) (*Scope, error) {
	if cmd.Process == nil {
		return nil, fmt.Errorf("native process not started")
	}
	pid := cmd.Process.Pid
	group, start, _, err := linuxStat(pid)
	if err != nil {
		return nil, err
	}
	if group != pid {
		return nil, fmt.Errorf("native process does not own its process group")
	}
	return &Scope{pid: pid, identity: start, platform: &linuxScope{group: group}}, nil
}
func linuxStat(pid int) (int, string, string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, "", "", err
	}
	cut := strings.LastIndex(string(raw), ")")
	if cut < 0 {
		return 0, "", "", fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(raw)[cut+1:])
	if len(fields) < 20 {
		return 0, "", "", fmt.Errorf("short process stat")
	}
	group, err := strconv.Atoi(fields[2])
	return group, fields[19], fields[0], err
}
func (p *linuxScope) Inspect(scope *Scope) Snapshot {
	result := scope.base()
	result.Platform = "linux"
	result.GroupID = p.group
	group, start, state, err := linuxStat(scope.pid)
	if err == nil && start != scope.identity {
		result.Reason = "native_root_pid_reused"
		return result
	}
	result.RootAlive = err == nil && group == p.group && start == scope.identity && state != "Z" && state != "X"
	if !result.RootAlive && !(result.RootExitVerified || (result.ControlledStop && result.WaitObserved)) {
		result.Reason = "native_root_identity_missing_or_changed"
		return result
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		result.Reason = "host_process_inventory_unavailable"
		return result
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == scope.pid {
			continue
		}
		group, _, state, err := linuxStat(pid)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			continue
		}
		if group == p.group && state != "Z" && state != "X" {
			result.ProcessIDs = append(result.ProcessIDs, pid)
		}
	}
	if result.RootAlive {
		group, start, _, err = linuxStat(scope.pid)
		if err != nil || group != p.group || start != scope.identity {
			result.Reason = "native_root_changed_during_inventory"
			return result
		}
	}
	result.Complete = true
	return result
}
func (p *linuxScope) StopChildren(ctx context.Context, scope *Scope) error {
	snapshot := p.Inspect(scope)
	if !snapshot.Complete {
		return fmt.Errorf("cannot stop an unverified process group: %s", snapshot.Reason)
	}
	for _, pid := range snapshot.ProcessIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		fd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			if err == unix.ESRCH {
				continue
			}
			return err
		}
		group, _, _, statErr := linuxStat(pid)
		if statErr == nil && group == p.group {
			err = unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0)
		}
		_ = unix.Close(fd)
		if err != nil && err != unix.ESRCH {
			return err
		}
	}
	return nil
}
func (p *linuxScope) Close() error { return nil }

func restorePlatform(snapshot Snapshot) (*Scope, error) {
	if snapshot.Platform != "linux" || snapshot.RootPID < 1 || snapshot.GroupID != snapshot.RootPID || snapshot.RootIdentity == "" {
		return nil, fmt.Errorf("invalid durable native Linux scope")
	}
	return &Scope{pid: snapshot.RootPID, identity: snapshot.RootIdentity, platform: &linuxScope{group: snapshot.GroupID}}, nil
}
