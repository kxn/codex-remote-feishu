package install

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskSchedulerRegistrationConsumesWrittenDefinition(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := filepath.Join(t.TempDir(), "Codex's $state & files")
	state := InstallState{InstanceID: "debug", BaseDir: baseDir, CurrentBinaryPath: filepath.Join(baseDir, "bin", "codex-remote.exe")}
	var registered bool
	withMockTaskSchedulerPowerShell(t, func(_ context.Context, script string) (string, error) {
		registered = true
		xmlPath := taskSchedulerXMLPathForInstance(state.BaseDir, state.InstanceID)
		quotedPath := "'" + strings.ReplaceAll(normalizeServicePathValue(xmlPath), "'", "''") + "'"
		if !strings.Contains(script, "-Xml (Get-Content -LiteralPath "+quotedPath+" -Raw)") {
			t.Fatal("registration did not consume the exact written XML definition")
		}
		for _, secondDefinition := range []string{"New-ScheduledTaskAction", "New-ScheduledTaskTrigger", "New-ScheduledTaskPrincipal", "-Settings"} {
			if strings.Contains(script, secondDefinition) {
				t.Fatalf("registration independently redefined task via %s", secondDefinition)
			}
		}
		data, err := os.ReadFile(xmlPath)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(decodeTaskSchedulerOutput(data), `encoding="UTF-16"`, `encoding="UTF-8"`, 1)
		var task struct {
			RegistrationInfo struct{ Description string }
			Triggers         struct {
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
			Settings struct {
				ExecutionTimeLimit, MultipleInstancesPolicy                            string
				DisallowStartIfOnBatteries, StopIfGoingOnBatteries, StartWhenAvailable bool
			}
			Actions struct {
				Exec struct{ Command, Arguments, WorkingDirectory string }
			}
		}
		if err := xml.Unmarshal([]byte(text), &task); err != nil {
			t.Fatal(err)
		}
		user, err := serviceCurrentUser()
		if err != nil {
			t.Fatal(err)
		}
		if task.Triggers.LogonTrigger.UserID != user || task.Principals.Principal.UserID != user || task.Principals.Principal.LogonType != "InteractiveToken" || task.Principals.Principal.RunLevel != "LeastPrivilege" {
			t.Fatalf("XML did not preserve current-user interactive least-privilege binding: %#v", task.Principals)
		}
		if task.Settings.ExecutionTimeLimit != "PT0S" || task.Settings.DisallowStartIfOnBatteries || task.Settings.StopIfGoingOnBatteries || !task.Settings.StartWhenAvailable || task.Settings.MultipleInstancesPolicy != "IgnoreNew" {
			t.Fatalf("registered XML has wrong daemon settings: %#v", task.Settings)
		}
		if task.Actions.Exec.Command != normalizeServicePathValue(state.CurrentBinaryPath) || task.Actions.Exec.WorkingDirectory != normalizeServicePathValue(state.BaseDir) || !strings.Contains(task.Actions.Exec.Arguments, "-config") || task.RegistrationInfo.Description != "codex-remote auto start at logon" {
			t.Fatalf("registered XML lost action/description: %#v", task)
		}
		return "", nil
	})
	if _, err := installTaskSchedulerLogonTask(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if !registered {
		t.Fatal("install did not register task")
	}
}
