package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func debugUsageEvents(surfaceID, formDefault, message string) []eventcontract.Event {
	return commandPageEvents(surfaceID, buildDebugRootPageView(install.InstallState{}, false, formDefault, "error", message))
}

func upgradeUsageEvents(surfaceID, formDefault, message string) []eventcontract.Event {
	return commandPageEvents(surfaceID, buildUpgradeRootPageView(install.InstallState{}, false, formDefault, "error", message))
}

func runCommandButton(label, commandText, style string, disabled bool) control.CommandCatalogButton {
	return control.FeishuLocalPageCommandButton(label, commandText, style, disabled)
}

func debugNoticeEvent(surfaceID, code, text string) eventcontract.Event {
	return eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: "Debug",
			Text:  text,
		},
	}
}

func upgradeNoticeEvent(surfaceID, code, text string) eventcontract.Event {
	return eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: "Upgrade",
			Text:  text,
		},
	}
}
