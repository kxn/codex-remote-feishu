package control

import "github.com/kxn/codex-remote-feishu/internal/core/upgradecontract"

type UpgradeCommandMode = upgradecontract.CommandMode

const (
	UpgradeCommandShowStatus = upgradecontract.CommandShowStatus
	UpgradeCommandShowTrack  = upgradecontract.CommandShowTrack
	UpgradeCommandSetTrack   = upgradecontract.CommandSetTrack
	UpgradeCommandLatest     = upgradecontract.CommandLatest
	UpgradeCommandCodex      = upgradecontract.CommandCodex
	UpgradeCommandDev        = upgradecontract.CommandDev
	UpgradeCommandLocal      = upgradecontract.CommandLocal
)

type ParsedUpgradeCommand = upgradecontract.ParsedCommand

type UpgradeCommandPresentation = upgradecontract.Presentation

const (
	UpgradeCommandPresentationPage    = upgradecontract.PresentationPage
	UpgradeCommandPresentationExecute = upgradecontract.PresentationExecute
)

func ParseFeishuUpgradeCommandText(text string) (ParsedUpgradeCommand, error) {
	return upgradecontract.ParseCommandText(text)
}

func FeishuUpgradeCommandPresentationForText(text string) (UpgradeCommandPresentation, bool) {
	return upgradecontract.PresentationForText(text)
}

func FeishuUpgradeCommandRunsImmediately(text string) bool {
	return upgradecontract.RunsImmediately(text)
}
