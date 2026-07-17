package daemon

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonAsyncRunnersDoNotDirectlyHandleUIEvents(t *testing.T) {
	cases := []struct {
		file string
		fn   string
	}{
		{"app_upgrade.go", "runUpgradeCheck"},
		{"app_upgrade_dev.go", "runDevUpgradeCheck"},
		{"app_upgrade_execute.go", "runPendingUpgradeStart"},
		{"app_upgrade_execute.go", "finishUpgradeStartFailure"},
		{"app_codex_upgrade.go", "runStandaloneCodexUpgrade"},
		{"app_cron_commands.go", "finishCronAsyncCommandLocked"},
		{"app_admin.go", "handleAdminWebCommand"},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		body := functionBodyForStructureTest(t, string(data), tc.fn)
		if strings.Contains(body, "handleUIEventsLocked(context.Background()") {
			t.Fatalf("%s:%s directly handles UI events from async path", tc.file, tc.fn)
		}
	}
}

func TestDaemonAsyncResultConsumersUseLockedQueueWhileHoldingAppLock(t *testing.T) {
	cases := []struct {
		file string
		fn   string
	}{
		{"app_upgrade.go", "runUpgradeCheck"},
		{"app_upgrade_dev.go", "runDevUpgradeCheck"},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		body := functionBodyForStructureTest(t, string(data), tc.fn)
		if strings.Contains(body, "queueDaemonAsyncUIEvents(events)") {
			t.Fatalf("%s:%s queues async UI events with non-locked helper while holding app lock", tc.file, tc.fn)
		}
	}
}
