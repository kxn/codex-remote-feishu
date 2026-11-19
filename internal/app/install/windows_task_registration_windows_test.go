package install

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt in on a Windows standard-user account to exercise the actual Task
// Scheduler, rather than a PowerShell mock. This creates and removes only a
// uniquely named test task; it never starts the task or touches a daemon task.
func TestNativeTaskSchedulerRegistrationPreservesSettings(t *testing.T) {
	if os.Getenv("CODEX_REMOTE_TEST_TASK_SCHEDULER") != "1" {
		t.Skip("set CODEX_REMOTE_TEST_TASK_SCHEDULER=1 on Windows to register a temporary task")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state := InstallState{
		InstanceID:        fmt.Sprintf("issue903-test-%d", time.Now().UnixNano()),
		BaseDir:           t.TempDir(),
		CurrentBinaryPath: filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"),
	}
	registered, err := installTaskSchedulerLogonTask(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := uninstallTaskSchedulerLogonTask(cleanupCtx, registered); err != nil {
			t.Error(err)
		}
	})
	output, err := taskSchedulerRunner(ctx, "/Query", "/TN", taskSchedulerTaskNameForInstance(state.InstanceID), "/XML")
	if err != nil {
		t.Fatal(err)
	}
	output = strings.Replace(output, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)
	var task struct {
		Settings struct {
			ExecutionTimeLimit, MultipleInstancesPolicy                            string
			DisallowStartIfOnBatteries, StopIfGoingOnBatteries, StartWhenAvailable bool
		}
		Triggers struct {
			LogonTrigger struct {
				UserID string `xml:"UserId"`
			}
		}
		Principals struct {
			Principal struct {
				UserID              string `xml:"UserId"`
				LogonType, RunLevel string
			}
		}
	}
	if err := xml.Unmarshal([]byte(output), &task); err != nil {
		t.Fatal(err)
	}
	if task.Settings.ExecutionTimeLimit != "PT0S" || task.Settings.DisallowStartIfOnBatteries || task.Settings.StopIfGoingOnBatteries || !task.Settings.StartWhenAvailable || task.Settings.MultipleInstancesPolicy != "IgnoreNew" {
		t.Fatalf("actual registered task lost settings: %#v", task.Settings)
	}
	if task.Triggers.LogonTrigger.UserID == "" || task.Triggers.LogonTrigger.UserID != task.Principals.Principal.UserID || task.Principals.Principal.LogonType != "InteractiveToken" || task.Principals.Principal.RunLevel != "LeastPrivilege" {
		t.Fatalf("actual registered task lost current-user scope: %#v", task)
	}
}
