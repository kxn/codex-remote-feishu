package install

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestStopInstallStateProcessTaskSchedulerAlsoTerminatesRecordedPID(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := t.TempDir()
	state := InstallState{
		InstanceID:     defaultInstanceID,
		BaseDir:        baseDir,
		StatePath:      defaultInstallStatePath(baseDir),
		ServiceManager: ServiceManagerTaskSchedulerLogon,
	}
	paths := relayruntime.Paths{
		PIDFile:      filepath.Join(baseDir, "daemon.pid"),
		IdentityFile: filepath.Join(baseDir, "identity.json"),
	}

	var taskCalls []string
	withMockTaskScheduler(t, func(_ context.Context, args ...string) (string, error) {
		taskCalls = append(taskCalls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "/Query" {
			return "Status: Ready\r\n", nil
		}
		return "", nil
	})

	var terminated []int
	var removed []string
	err := stopInstallStateProcess(context.Background(), state, paths, stopInstallStateOptions{
		StopGrace:    time.Second,
		PollInterval: time.Millisecond,
	}, runtimeControlHooks{
		Sleep: func(time.Duration) {},
		ReadPID: func(path string) (int, error) {
			if path != paths.PIDFile {
				t.Fatalf("ReadPID path = %q, want %q", path, paths.PIDFile)
			}
			return 4321, nil
		},
		TerminateProcess: func(pid int, grace time.Duration) error {
			terminated = append(terminated, pid)
			if grace != time.Second {
				t.Fatalf("grace = %s, want 1s", grace)
			}
			return nil
		},
		RemoveFile: func(path string) error {
			removed = append(removed, path)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("stopInstallStateProcess: %v", err)
	}

	taskName := taskSchedulerTaskNameForInstance(defaultInstanceID)
	wantTaskCalls := []string{
		"/End /TN " + taskName,
		"/Query /TN " + taskName + " /FO LIST /V",
	}
	if !reflect.DeepEqual(taskCalls, wantTaskCalls) {
		t.Fatalf("task calls = %#v, want %#v", taskCalls, wantTaskCalls)
	}
	if !reflect.DeepEqual(terminated, []int{4321}) {
		t.Fatalf("terminated = %#v, want [4321]", terminated)
	}
	if !reflect.DeepEqual(removed, []string{paths.PIDFile, paths.IdentityFile}) {
		t.Fatalf("removed = %#v, want pid and identity", removed)
	}
}

func TestTerminateInstallStatePIDIgnoresMissingPIDFile(t *testing.T) {
	err := terminateInstallStatePID(relayruntime.Paths{PIDFile: "missing.pid"}, time.Second, runtimeControlHooks{
		ReadPID: func(string) (int, error) {
			return 0, os.ErrNotExist
		},
	})
	if err != nil {
		t.Fatalf("terminateInstallStatePID: %v", err)
	}
}
