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
