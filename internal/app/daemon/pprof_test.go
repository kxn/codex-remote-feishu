package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestPprofBindAddrForDebugSettingsDisabledByDefault(t *testing.T) {
	if got := pprofBindAddrForDebugSettings(config.DebugSettings{}); got != "" {
		t.Fatalf("pprofBindAddrForDebugSettings() = %q, want empty", got)
	}
}

func TestPprofBindAddrForDebugSettingsUsesDefaultsWhenEnabled(t *testing.T) {
	got := pprofBindAddrForDebugSettings(config.DebugSettings{
		Pprof: &config.PprofSettings{Enabled: true},
	})
	if got != "127.0.0.1:17501" {
		t.Fatalf("pprofBindAddrForDebugSettings() = %q, want 127.0.0.1:17501", got)
	}
}
