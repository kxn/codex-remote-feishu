package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const (
	cronScheduleTypeDaily    = "每天定时"
	cronScheduleTypeInterval = "间隔运行"
)

type cronIntervalChoice struct {
	Label   string
	Minutes int
}

var cronIntervalChoices = []cronIntervalChoice{
	{Label: "5分钟", Minutes: 5},
	{Label: "10分钟", Minutes: 10},
	{Label: "15分钟", Minutes: 15},
	{Label: "30分钟", Minutes: 30},
	{Label: "1小时", Minutes: 60},
	{Label: "2小时", Minutes: 120},
	{Label: "4小时", Minutes: 240},
	{Label: "6小时", Minutes: 360},
	{Label: "12小时", Minutes: 720},
	{Label: "24小时", Minutes: 1440},
}

type cronCommandMode string

const (
	cronCommandShow         cronCommandMode = "show"
	cronCommandRepair       cronCommandMode = "repair"
	cronCommandReload       cronCommandMode = "reload"
	cronCommandMigrateOwner cronCommandMode = "migrate-owner"
)

type parsedCronCommand struct {
	Mode cronCommandMode
}

func parseCronCommandText(text string) (parsedCronCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return parsedCronCommand{}, fmt.Errorf("缺少 /cron 子命令。")
	}
	fields := strings.Fields(strings.ToLower(trimmed))
	if len(fields) == 0 || fields[0] != "/cron" {
		return parsedCronCommand{}, fmt.Errorf("不支持的 /cron 子命令。")
	}
	switch len(fields) {
	case 1:
		return parsedCronCommand{Mode: cronCommandShow}, nil
	case 2:
		switch fields[1] {
		case "repair":
			return parsedCronCommand{Mode: cronCommandRepair}, nil
		case "reload":
			return parsedCronCommand{Mode: cronCommandReload}, nil
		case "migrate-owner":
			return parsedCronCommand{Mode: cronCommandMigrateOwner}, nil
		}
		return parsedCronCommand{}, fmt.Errorf("`/cron` 只支持查看状态、执行 `/cron repair`、`/cron reload` 或 `/cron migrate-owner`。")
	default:
		return parsedCronCommand{}, fmt.Errorf("`/cron repair` / `/cron reload` / `/cron migrate-owner` 不接受额外参数。")
	}
}

func cronUsageEvents(surfaceID, message string) []control.UIEvent {
	events := []control.UIEvent{}
	if strings.TrimSpace(message) != "" {
		events = append(events, cronNoticeEvent(surfaceID, "cron_usage_error", message))
	}
	events = append(events, control.UIEvent{
		Kind:                       control.UIEventFeishuDirectCommandCatalog,
		SurfaceSessionID:           surfaceID,
		FeishuDirectCommandCatalog: buildCronStatusCatalog(nil, cronOwnerView{}, ""),
	})
	return events
}

func buildCronStatusCatalog(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string) *control.FeishuDirectCommandCatalog {
	summaryLines := []string{}
	if stateValue == nil || !cronStateHasBinding(stateValue) {
		summaryLines = append(summaryLines, "当前实例还没有初始化 Cron 多维表格。执行 `/cron repair` 后会创建配置表。")
	} else {
		tableLine := fmt.Sprintf("配置表：%s", firstNonEmpty(strings.TrimSpace(stateValue.Bitable.AppURL), strings.TrimSpace(stateValue.Bitable.AppToken)))
		if url := strings.TrimSpace(stateValue.Bitable.AppURL); url != "" {
			tableLine = fmt.Sprintf("配置表：[%s](%s)", "打开 Cron 配置表", url)
		}
		summaryLines = append(summaryLines,
			fmt.Sprintf("实例：%s", firstNonEmpty(strings.TrimSpace(stateValue.InstanceLabel), "unknown")),
			tableLine,
		)
		if !stateValue.LastWorkspaceSyncAt.IsZero() {
			summaryLines = append(summaryLines, fmt.Sprintf("最近工作区同步：%s", stateValue.LastWorkspaceSyncAt.UTC().Format(time.RFC3339)))
		}
		if !stateValue.LastReloadAt.IsZero() {
			summaryLines = append(summaryLines, fmt.Sprintf("最近 reload：%s", stateValue.LastReloadAt.UTC().Format(time.RFC3339)))
		}
		if strings.TrimSpace(stateValue.LastReloadSummary) != "" {
			summaryLines = append(summaryLines, "最近 reload 摘要："+strings.TrimSpace(stateValue.LastReloadSummary))
		}
	}
	if strings.TrimSpace(ownerView.StatusLabel) != "" {
		summaryLines = append(summaryLines, "Owner 状态："+strings.TrimSpace(ownerView.StatusLabel))
	}
	if strings.TrimSpace(ownerView.Detail) != "" {
		summaryLines = append(summaryLines, strings.TrimSpace(ownerView.Detail))
	}
	if strings.TrimSpace(ownerView.NextAction) != "" {
		summaryLines = append(summaryLines, "下一步："+strings.TrimSpace(ownerView.NextAction))
	}
	if strings.TrimSpace(extraSummary) != "" {
		summaryLines = append(summaryLines, strings.TrimSpace(extraSummary))
	}
	return &control.FeishuDirectCommandCatalog{
		Title:        "Cron",
		Summary:      strings.Join(summaryLines, "\n"),
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{
			{
				Title: "快捷操作",
				Entries: []control.CommandCatalogEntry{{
					Buttons: []control.CommandCatalogButton{
						runCommandButton("修复配置表", "/cron repair", "primary", false),
						runCommandButton("重新加载配置", "/cron reload", "", false),
						runCommandButton("迁移 owner", "/cron migrate-owner", "", false),
					},
				}},
			},
			{
				Title: "手动输入",
				Entries: []control.CommandCatalogEntry{{
					Commands:    []string{"/cron", "/cron repair", "/cron reload", "/cron migrate-owner"},
					Description: "直接发送 `/cron` 查看当前绑定与 owner 状态；`/cron repair` 修复配置表并同步工作区；`/cron reload` 重新加载任务；`/cron migrate-owner` 显式把 owner 切到当前 surface 对应 bot。",
					Form:        control.FeishuCommandFormWithDefault(control.FeishuCommandCron, ""),
				}},
			},
		},
	}
}

func cronNoticeEvent(surfaceID, code, text string) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: "Cron",
			Text:  text,
		},
	}
}
