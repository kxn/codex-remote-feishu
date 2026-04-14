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
	switch strings.TrimSpace(commandID) {
	case FeishuCommandFollow:
		return NormalizeFeishuCommandMenuStage(stage) == FeishuCommandMenuStageVSCodeWorking
	case FeishuCommandNew:
		return NormalizeFeishuCommandMenuStage(stage) == FeishuCommandMenuStageNormalWorking
	default:
		return true
	}
}
