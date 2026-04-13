//go:build windows

package relayruntime

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsStillActive = 259

const (
	th32csSnapProcess = 0x00000002
	invalidHandle     = ^uintptr(0)
)

var (
	modKernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = modKernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = modKernel32.NewProc("Process32FirstW")
	procProcess32NextW           = modKernel32.NewProc("Process32NextW")
)

type processEntry32 struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PcPriClassBase  int32
	Flags           uint32
	ExeFile         [windows.MAX_PATH]uint16
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == windowsStillActive
}

func terminateProcess(pid int, grace time.Duration) error {
	if pid <= 0 {
		return nil
	}
	pids, err := processTreePostOrder(pid)
	if err != nil {
		pids = []int{pid}
	}
	type openedHandle struct {
		pid    int
		handle windows.Handle
		root   bool
	}
	handles := make([]openedHandle, 0, len(pids))
	for _, currentPID := range pids {
		if currentPID <= 0 {
			continue
		}
		handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(currentPID))
		if err != nil {
			if currentPID == pid && processAlive(currentPID) {
				return err
			}
			continue
		}
		if !processAlive(currentPID) {
			windows.CloseHandle(handle)
			continue
		}
		if err := windows.TerminateProcess(handle, 1); err != nil {
			windows.CloseHandle(handle)
			if currentPID == pid && processAlive(currentPID) {
				return err
			}
			continue
		}
		handles = append(handles, openedHandle{pid: currentPID, handle: handle, root: currentPID == pid})
	}
	defer func() {
		for _, item := range handles {
			windows.CloseHandle(item.handle)
		}
	}()
	for _, item := range handles {
		timeout := uint32(200)
		if item.root {
			timeout = waitTimeoutMillis(grace)
		}
		waitResult, err := windows.WaitForSingleObject(item.handle, timeout)
		if err != nil {
			if item.root {
				return err
			}
			continue
		}
		switch waitResult {
		case uint32(windows.WAIT_OBJECT_0):
			continue
		case uint32(windows.WAIT_TIMEOUT):
			if !processAlive(item.pid) {
				continue
			}
			if item.root {
				return fmt.Errorf("process %d still alive after terminate timeout", item.pid)
			}
		default:
			if item.root {
				return fmt.Errorf("wait for process %d exit returned %d", item.pid, waitResult)
			}
		}
	}
	if processAlive(pid) {
		return fmt.Errorf("process %d still alive after terminate", pid)
	}
	return nil
}

func prepareDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
}

func waitTimeoutMillis(grace time.Duration) uint32 {
	if grace <= 0 {
		return 0
	}
	ms := grace.Milliseconds()
	if ms <= 0 {
		return 1
	}
	if ms > int64(^uint32(0)>>1) {
		return ^uint32(0) >> 1
	}
	return uint32(ms)
}

func processTreePostOrder(rootPID int) ([]int, error) {
	if rootPID <= 0 {
		return nil, nil
	}
	relations, err := snapshotProcessParents()
	if err != nil {
		return nil, err
	}
	ordered := make([]int, 0, 8)
	seen := map[int]bool{}
	var visit func(int)
	visit = func(pid int) {
		if pid <= 0 || seen[pid] {
			return
		}
		seen[pid] = true
		for _, child := range relations[pid] {
			visit(child)
		}
		ordered = append(ordered, pid)
	}
	visit(rootPID)
	return ordered, nil
}

func snapshotProcessParents() (map[int][]int, error) {
	handle, _, callErr := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if handle == invalidHandle {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return nil, callErr
		}
		return nil, syscall.EINVAL
	}
	snapshot := windows.Handle(handle)
	defer windows.CloseHandle(snapshot)

	entry := processEntry32{Size: uint32(unsafe.Sizeof(processEntry32{}))}
	ret, _, callErr := procProcess32FirstW.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return nil, callErr
		}
		return map[int][]int{}, nil
	}

	relations := map[int][]int{}
	for {
		parent := int(entry.ParentProcessID)
		child := int(entry.ProcessID)
		if child > 0 {
			relations[parent] = append(relations[parent], child)
		}
		ret, _, callErr = procProcess32NextW.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
		if ret != 0 {
			continue
		}
		if callErr == syscall.ERROR_NO_MORE_FILES || callErr == windows.ERROR_NO_MORE_FILES {
			break
		}
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return nil, callErr
		}
		break
	}
	return relations, nil
}
