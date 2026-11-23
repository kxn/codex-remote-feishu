package privatechat

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPrivateCatalogKeepsLegacyChoicesWithoutRetiredEntries(t *testing.T) {
	ctx := control.CatalogContext{PrivateApplication: true, MenuStage: string(control.FeishuCommandMenuStageNormalWorking), InstanceID: "i"}
	for _, id := range []string{control.FeishuCommandCron, control.FeishuCommandVSCodeMigrate, control.FeishuCommandFollow} {
		if _, ok := control.ResolveFeishuCommandDisplayFamily(id, false, ctx); ok {
			t.Fatalf("retired entry %s visible", id)
		}
	}
	for _, id := range []string{control.FeishuCommandAutoWhip, control.FeishuCommandAutoContinue, control.FeishuCommandNew} {
		if _, ok := control.ResolveFeishuCommandDisplayFamily(id, false, ctx); !ok {
			t.Fatalf("preserved entry %s missing", id)
		}
	}
	page := control.BuildFeishuCommandConfigPageView(control.FeishuCatalogConfigView{PrivateApplication: true, CommandID: control.FeishuCommandMode})
	for _, section := range page.Sections {
		for _, entry := range section.Entries {
			for _, button := range entry.Buttons {
				if strings.Contains(button.CommandText, "vscode") {
					t.Fatalf("retired mode visible: %#v", button)
				}
			}
		}
	}
}
