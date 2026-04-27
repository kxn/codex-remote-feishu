package control

import "strings"

type FeishuCommandMenuStage string

const (
	FeishuCommandMenuStageDetached      FeishuCommandMenuStage = "detached"
	FeishuCommandMenuStageNormalWorking FeishuCommandMenuStage = "normal_working"
	FeishuCommandMenuStageVSCodeWorking FeishuCommandMenuStage = "vscode_working"
)

func NormalizeFeishuCommandMenuStage(stage string) FeishuCommandMenuStage {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case string(FeishuCommandMenuStageNormalWorking):
		return FeishuCommandMenuStageNormalWorking
	case string(FeishuCommandMenuStageVSCodeWorking):
		return FeishuCommandMenuStageVSCodeWorking
	default:
		return FeishuCommandMenuStageDetached
	}
}

func FeishuCommandVisibleInMenuStage(commandID, stage string) bool {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return false
	}
	visible := false
	for _, profile := range feishuCommandDisplayProfiles {
		family, ok := profile.FamilyProfile(commandID)
		if !ok {
			continue
		}
		visible = true
		if family.MenuVisibleInStage(stage) {
			return true
		}
	}
	if visible {
		return false
	}
	return true
}
