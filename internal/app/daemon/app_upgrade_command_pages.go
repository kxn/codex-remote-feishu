package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func buildDebugRootPageView(stateValue install.InstallState, checkInFlight bool, formDefault, statusKind, statusText string) control.FeishuCommandPageView {
	return control.FeishuCommandPageView{
		CommandID:    control.FeishuCommandDebug,
		StatusKind:   strings.TrimSpace(statusKind),
		StatusText:   strings.TrimSpace(statusText),
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{
			{
				Title: "调试",
				Entries: []control.CommandCatalogEntry{{
					Buttons: []control.CommandCatalogButton{
						runCommandButton("管理页外链", "/debug admin", "primary", false),
					},
				}},
			},
		},
	}
}

func buildUpgradeRootPageView(stateValue install.InstallState, formDefault, statusKind, statusText string) control.FeishuCommandPageView {
	quickButtons := []control.CommandCatalogButton{
		runCommandButton("查看 Track", "/upgrade track", "", false),
		runCommandButton("检查/继续升级", "/upgrade latest", "primary", false),
		runCommandButton("开发构建", "/upgrade dev", "", false),
	}
	if install.CurrentBuildAllowsLocalUpgrade() {
		quickButtons = append(quickButtons, runCommandButton("本地升级", "/upgrade local", "", false))
	}
	return control.FeishuCommandPageView{
		CommandID:    control.FeishuCommandUpgrade,
		StatusKind:   strings.TrimSpace(statusKind),
		StatusText:   strings.TrimSpace(statusText),
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{
			{
				Title: "升级",
				Entries: []control.CommandCatalogEntry{{
					Buttons: quickButtons,
				}},
			},
		},
	}
}

func buildUpgradeTrackPageView(stateValue install.InstallState) control.FeishuCommandPageView {
	currentTrack := strings.TrimSpace(string(stateValue.CurrentTrack))
	return control.FeishuCommandPageView{
		CommandID:       control.FeishuCommandUpgrade,
		Title:           "Upgrade Track",
		Breadcrumbs:     control.FeishuCommandBreadcrumbsForCommand(control.FeishuCommandUpgrade, "Track"),
		SummarySections: commandCatalogSummarySections(buildTrackSummaryLines(stateValue)...),
		Interactive:     true,
		DisplayStyle:    control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{
			{
				Title: "切换 Track",
				Entries: []control.CommandCatalogEntry{{
					Buttons: buildTrackCommandButtons(currentTrack),
				}},
			},
		},
		RelatedButtons: control.FeishuCommandBackToRootButtons(control.FeishuCommandUpgrade),
	}
}

func buildTrackSummaryLines(stateValue install.InstallState) []string {
	return []string{
		fmt.Sprintf("当前 Track：%s", firstNonEmpty(string(stateValue.CurrentTrack), "unknown")),
	}
}
