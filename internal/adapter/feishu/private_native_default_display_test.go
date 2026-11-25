package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestSnapshotDisplaysPrivateNativeModelInheritance(t *testing.T) {
	got := formatSnapshotEffectivePromptPlain(control.PromptRouteSummary{EffectiveModelSource: "native_default"})
	if !strings.Contains(got, "模型 跟随原生配置") {
		t.Fatalf("missing native inheritance: %q", got)
	}
}
