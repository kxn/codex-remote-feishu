package install

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/user"
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
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	// Exported XML may preserve the trigger account name while normalizing the
	// principal to a SID. Compare both to the current process user's native SID.
	triggerSID := registeredTaskAccountSID(t, task.Triggers.LogonTrigger.UserID)
	principalSID := registeredTaskAccountSID(t, task.Principals.Principal.UserID)
	if triggerSID != current.Uid || principalSID != current.Uid || task.Principals.Principal.LogonType != "InteractiveToken" {
		t.Fatalf("actual registered task lost current-user scope: %#v", task)
	}
	// Task Scheduler can omit the default RunLevel from exported XML. Read the
	// native task definition instead: TASK_RUNLEVEL_LUA is 0; highest is 1.
	name := strings.ReplaceAll(taskSchedulerTaskNameForInstance(state.InstanceID), "'", "''")
	level, err := taskSchedulerPowerShellRunner(ctx, fmt.Sprintf(
		"$ErrorActionPreference='Stop'; $scheduler = New-Object -ComObject Schedule.Service; $scheduler.Connect(); $task = $scheduler.GetFolder('\\').GetTask('%s'); [int]$task.Definition.Principal.RunLevel", name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(level) != "0" {
		t.Fatalf("actual registered task must use least privilege, got native RunLevel %q (XML %q)", level, task.Principals.Principal.RunLevel)
	}
}

func registeredTaskAccountSID(t *testing.T, account string) string {
	t.Helper()
	if strings.TrimSpace(account) == "" {
		t.Fatal("actual registered task omitted its account identity")
	}
	var resolved *user.User
	var err error
	if strings.HasPrefix(strings.ToUpper(account), "S-") {
		resolved, err = user.LookupId(account)
	} else {
		resolved, err = user.Lookup(account)
	}
	if err != nil {
		t.Fatalf("resolve registered task account %q: %v", account, err)
	}
	return resolved.Uid
}
