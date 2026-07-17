package daemon

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonFeishuSetupUsesSingleFacadeHook(t *testing.T) {
	for _, file := range []string{
		"admin_feishu_autoconfig.go",
		"admin_feishu_onboarding.go",
		"admin_onboarding_workflow.go",
		"runtime_state.go",
		"app.go",
	} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		source := string(data)
		for _, legacy := range []string{
			"planFeishuAppAutoConfig",
			"applyFeishuAppAutoConfig",
			"publishFeishuAppAutoConfig",
			"getFeishuLongConnectionStatus",
			"feishuRuntime.setup",
			"feishuSetupClient",
		} {
			if strings.Contains(source, legacy) {
				t.Fatalf("%s still contains legacy setup hook %s", file, legacy)
			}
		}
	}
}
