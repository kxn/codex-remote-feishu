package control

import "github.com/kxn/codex-remote-feishu/internal/core/upgradecontract"

func FeishuUpgradeCommandRunsImmediately(text string) bool {
	return upgradecontract.RunsImmediately(text)
}
