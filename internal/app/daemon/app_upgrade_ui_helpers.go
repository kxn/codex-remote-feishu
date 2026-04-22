package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func debugUsageEvents(surfaceID, formDefault, message string) []control.UIEvent {
	return commandPageEvents(surfaceID, buildDebugRootPageView(install.InstallState{}, false, formDefault, "error", message))
}

func upgradeUsageEvents(surfaceID, formDefault, message string) []control.UIEvent {
	return commandPageEvents(surfaceID, buildUpgradeRootPageView(install.InstallState{}, formDefault, "error", message))
}

func runCommandButton(label, commandText, style string, disabled bool) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label:       label,
		Kind:        control.CommandCatalogButtonAction,
		CommandText: commandText,
		Style:       style,
		Disabled:    disabled,
	}
}

func debugNoticeEvent(surfaceID, code, text string) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: "Debug",
			Text:  text,
		},
	}
}

func upgradeNoticeEvent(surfaceID, code, text string) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: "Upgrade",
			Text:  text,
		},
	}
}
