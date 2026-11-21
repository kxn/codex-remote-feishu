//go:build windows

package hostinventory

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsScope struct {
	job  windows.Handle
	root windows.Handle
	name string
}

func preparePlatform(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED
}
func bindPlatform(cmd *exec.Cmd) (*Scope, error) {
	if cmd.Process == nil {
		return nil, fmt.Errorf("native process not started")
	}
	root, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return nil, err
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(root, &creation, &exit, &kernel, &user); err != nil {
		windows.CloseHandle(root)
		return nil, err
	}
	identity := fmt.Sprintf("%d:%d", creation.HighDateTime, creation.LowDateTime)
	name := fmt.Sprintf("Local\\codex-remote-native-%d-%d-%d", cmd.Process.Pid, creation.HighDateTime, creation.LowDateTime)
	utf16Name, err := windows.UTF16PtrFromString(name)
	if err != nil {
		windows.CloseHandle(root)
		return nil, err
	}
	job, err := windows.CreateJobObject(nil, utf16Name)
	if err != nil {
		windows.CloseHandle(root)
		return nil, err
	}
	if err := windows.AssignProcessToJobObject(job, root); err != nil {
		windows.CloseHandle(root)
		windows.CloseHandle(job)
		return nil, err
	}
	platform := &windowsScope{job: job, root: root, name: name}
	if err := platform.adoptEarlyChildren(uint32(cmd.Process.Pid)); err != nil {
		platform.Close()
		return nil, err
	}
	if err := resumeNativeThread(uint32(cmd.Process.Pid)); err != nil {
		platform.Close()
		return nil, err
	}
	return &Scope{pid: cmd.Process.Pid, identity: identity, platform: platform}, nil
}
func (p *windowsScope) adoptEarlyChildren(root uint32) error {
	handle, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	parents := map[uint32]uint32{}
	for err = windows.Process32First(handle, &entry); err == nil; err = windows.Process32Next(handle, &entry) {
		parents[entry.ProcessID] = entry.ParentProcessID
	}
	owned := map[uint32]bool{root: true}
	for changed := true; changed; {
		changed = false
		for pid, parent := range parents {
			if !owned[pid] && owned[parent] {
				owned[pid] = true
				changed = true
			}
		}
	}
	for pid := range owned {
		if pid == root {
			continue
		}
		process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			return fmt.Errorf("early native child cannot be inspected: %w", err)
		}
		inJob, err := isProcessInJob(process, p.job)
		if err == nil && !inJob {
			err = windows.AssignProcessToJobObject(p.job, process)
		}
		windows.CloseHandle(process)
		if err != nil {
			return err
		}
	}
	return nil
}
func (p *windowsScope) processIDs() ([]int, error) {
	for count := 128; count <= 16384; count *= 2 {
		// JOBOBJECT_BASIC_PROCESS_ID_LIST: two uint32 counts, then ULONG_PTR IDs.
		raw := make([]uintptr, count+2)
		var returned uint32
		err := windows.QueryInformationJobObject(p.job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&raw[0])), uint32(len(raw))*uint32(unsafe.Sizeof(uintptr(0))), &returned)
		if err == windows.ERROR_MORE_DATA {
			continue
		}
		if err != nil {
			return nil, err
		}
		counts := (*[2]uint32)(unsafe.Pointer(&raw[0]))
		if int(counts[0]) > count {
			continue
		}
		base := uintptr(unsafe.Pointer(&raw[0])) + 8
		var ids []int
		for i := 0; i < int(counts[1]); i++ {
			ids = append(ids, int(*(*uintptr)(unsafe.Pointer(base + uintptr(i)*unsafe.Sizeof(uintptr(0))))))
		}
		return ids, nil
	}
	return nil, fmt.Errorf("native job exceeds bounded inventory capacity")
}
func (p *windowsScope) Inspect(scope *Scope) Snapshot {
	result := scope.base()
	result.Platform = "windows"
	result.JobName = p.name
	var code uint32
	err := windows.GetExitCodeProcess(p.root, &code)
	result.RootAlive = err == nil && code == 259
	if !result.RootAlive && !(result.RootExitVerified || (result.ControlledStop && result.WaitObserved)) {
		result.Reason = "native_root_exit_not_proven_controlled"
		return result
	}
	ids, err := p.processIDs()
	if err != nil {
		result.Reason = "native_job_inventory_unavailable"
		return result
	}
	for _, pid := range ids {
		if pid != scope.pid {
			result.ProcessIDs = append(result.ProcessIDs, pid)
		}
	}
	var finalCode uint32
	if err := windows.GetExitCodeProcess(p.root, &finalCode); err != nil || (finalCode != 259 && !(scope.controlled.Load() && scope.waited.Load())) {
		result.Reason = "native_root_changed_during_inventory"
		return result
	}
	result.Complete = true
	return result
}
func (p *windowsScope) StopChildren(ctx context.Context, scope *Scope) error {
	snapshot := p.Inspect(scope)
	if !snapshot.Complete {
		return fmt.Errorf("native job not verified")
	}
	for _, pid := range snapshot.ProcessIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		process, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if err != nil {
			continue
		}
		owned, err := isProcessInJob(process, p.job)
		if err == nil && owned {
			err = windows.TerminateProcess(process, 1)
		}
		windows.CloseHandle(process)
		if err != nil {
			return err
		}
	}
	return nil
}
func (p *windowsScope) Close() error {
	_ = windows.CloseHandle(p.root)
	return windows.CloseHandle(p.job)
}

var nativeIsProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func isProcessInJob(process, job windows.Handle) (bool, error) {
	var owned int32
	result, _, err := nativeIsProcessInJob.Call(uintptr(process), uintptr(job), uintptr(unsafe.Pointer(&owned)))
	if result == 0 {
		return false, err
	}
	return owned != 0, nil
}

var nativeOpenJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")

func restorePlatform(snapshot Snapshot) (*Scope, error) {
	if snapshot.Platform != "windows" || snapshot.RootPID < 1 || snapshot.RootIdentity == "" || snapshot.JobName == "" {
		return nil, fmt.Errorf("invalid durable Windows native job")
	}
	name, err := windows.UTF16PtrFromString(snapshot.JobName)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := nativeOpenJobObject.Call(0x0004|0x0008, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, callErr
	}
	root, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(snapshot.RootPID))
	if err == nil {
		var creation, exit, kernel, user windows.Filetime
		if err := windows.GetProcessTimes(root, &creation, &exit, &kernel, &user); err != nil || fmt.Sprintf("%d:%d", creation.HighDateTime, creation.LowDateTime) != snapshot.RootIdentity {
			windows.CloseHandle(root)
			root = 0
		}
	}
	return &Scope{pid: snapshot.RootPID, identity: snapshot.RootIdentity, platform: &windowsScope{job: windows.Handle(handle), root: root, name: snapshot.JobName}}, nil
}

func resumeNativeThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		return err
	}
	return fmt.Errorf("native suspended primary thread not found")
}
