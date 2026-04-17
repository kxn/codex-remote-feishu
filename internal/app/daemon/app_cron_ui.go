package daemon

import (
	"fmt"
	"sort"
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
	cronCommandMenu   cronCommandMode = "menu"
	cronCommandStatus cronCommandMode = "status"
	cronCommandList   cronCommandMode = "list"
	cronCommandEdit   cronCommandMode = "edit"
	cronCommandRepair cronCommandMode = "repair"
	cronCommandReload cronCommandMode = "reload"
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
		return parsedCronCommand{Mode: cronCommandMenu}, nil
	case 2:
		switch fields[1] {
		case "status":
			return parsedCronCommand{Mode: cronCommandStatus}, nil
		case "list":
			return parsedCronCommand{Mode: cronCommandList}, nil
		case "edit":
			return parsedCronCommand{Mode: cronCommandEdit}, nil
		case "repair":
			return parsedCronCommand{Mode: cronCommandRepair}, nil
		case "reload":
			return parsedCronCommand{Mode: cronCommandReload}, nil
		}
		return parsedCronCommand{}, fmt.Errorf("`/cron` 只支持 `/cron status`、`/cron list`、`/cron edit`、`/cron reload` 或 `/cron repair`。")
	default:
		return parsedCronCommand{}, fmt.Errorf("`/cron status` / `/cron list` / `/cron edit` / `/cron reload` / `/cron repair` 不接受额外参数。")
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
		FeishuDirectCommandCatalog: buildCronMenuCatalog(nil, cronOwnerView{}, ""),
	})
	return events
}

func buildCronMenuCatalog(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string) *control.FeishuDirectCommandCatalog {
	summaryLines := []string{"选择 Cron 的下一步操作。"}
	if strings.TrimSpace(ownerView.StatusLabel) != "" {
		summaryLines = append(summaryLines, "当前状态："+strings.TrimSpace(ownerView.StatusLabel))
	}
	if stateValue != nil {
		summaryLines = append(summaryLines, fmt.Sprintf("实例：%s", firstNonEmpty(strings.TrimSpace(stateValue.InstanceLabel), "unknown")))
		if line := cronLoadedJobCountLine(stateValue, ownerView); line != "" {
			summaryLines = append(summaryLines, line)
		}
	}
	if strings.TrimSpace(ownerView.NextAction) != "" {
		summaryLines = append(summaryLines, "下一步："+strings.TrimSpace(ownerView.NextAction))
	}
	if strings.TrimSpace(extraSummary) != "" {
		summaryLines = append(summaryLines, strings.TrimSpace(extraSummary))
	}
	primaryCommand := cronPrimaryMenuCommand(stateValue, ownerView)
	canEdit := cronCanEdit(stateValue)
	canReload := cronCanReload(stateValue, ownerView)
	return &control.FeishuDirectCommandCatalog{
		Title:        "Cron",
		Summary:      strings.Join(summaryLines, "\n"),
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{
			{
				Title: "查看与编辑",
				Entries: []control.CommandCatalogEntry{{
					Buttons: []control.CommandCatalogButton{
						runCommandButton("当前状态", "/cron status", cronPrimaryButtonStyle(primaryCommand, "/cron status"), false),
						runCommandButton("当前任务", "/cron list", cronPrimaryButtonStyle(primaryCommand, "/cron list"), false),
						runCommandButton("打开配置", "/cron edit", cronPrimaryButtonStyle(primaryCommand, "/cron edit"), !canEdit),
					},
				}},
			},
			{
				Title: "应用与维护",
				Entries: []control.CommandCatalogEntry{{
					Buttons: []control.CommandCatalogButton{
						runCommandButton("重新加载", "/cron reload", cronPrimaryButtonStyle(primaryCommand, "/cron reload"), !canReload),
						runCommandButton("修复配置", "/cron repair", cronPrimaryButtonStyle(primaryCommand, "/cron repair"), false),
					},
				}},
			},
			cronManualCommandSection(),
		},
	}
}

func buildCronStatusCatalog(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string) *control.FeishuDirectCommandCatalog {
	summaryLines := []string{}
	if stateValue == nil || !cronStateHasBinding(stateValue) {
		summaryLines = append(summaryLines, "当前实例还没有初始化 Cron 配置表。执行 `/cron repair` 后会创建配置表。")
	} else {
		summaryLines = append(summaryLines,
			fmt.Sprintf("实例：%s", firstNonEmpty(strings.TrimSpace(stateValue.InstanceLabel), "unknown")),
			cronBindingLinkLine(stateValue),
		)
		if line := cronLoadedJobCountLine(stateValue, ownerView); line != "" {
			summaryLines = append(summaryLines, line)
		}
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
		summaryLines = append(summaryLines, "当前状态："+strings.TrimSpace(ownerView.StatusLabel))
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
		Title:        "Cron 状态",
		Summary:      strings.Join(summaryLines, "\n"),
		Interactive:  false,
		DisplayStyle: control.CommandCatalogDisplayDefault,
	}
}

func buildCronListCatalog(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string) *control.FeishuDirectCommandCatalog {
	summaryLines := []string{}
	switch {
	case stateValue == nil || !cronStateHasBinding(stateValue):
		summaryLines = append(summaryLines, "当前实例还没有初始化 Cron 配置表。执行 `/cron repair` 后会创建配置表。")
	case !cronOwnerAllowsLoadedJobs(ownerView.Status):
		summaryLines = append(summaryLines,
			"当前 Cron 绑定需要先修复后才能确认有效任务。",
			"执行 `/cron repair` 完成接管后，再执行 `/cron reload` 重新加载任务。",
		)
	case len(stateValue.Jobs) == 0:
		summaryLines = append(summaryLines, "当前没有已加载的 Cron 任务。编辑表格后执行 `/cron reload` 生效。")
	default:
		summaryLines = append(summaryLines, fmt.Sprintf("当前已加载 %d 条任务：", len(stateValue.Jobs)))
		summaryLines = append(summaryLines, cronLoadedJobLines(stateValue.Jobs)...)
	}
	if strings.TrimSpace(extraSummary) != "" {
		summaryLines = append(summaryLines, "", strings.TrimSpace(extraSummary))
	}
	return &control.FeishuDirectCommandCatalog{
		Title:        "Cron 任务",
		Summary:      strings.Join(summaryLines, "\n"),
		Interactive:  false,
		DisplayStyle: control.CommandCatalogDisplayDefault,
	}
}

func buildCronEditCatalog(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string) *control.FeishuDirectCommandCatalog {
	summaryLines := []string{}
	if stateValue == nil || !cronStateHasBinding(stateValue) {
		summaryLines = append(summaryLines, "当前还没有可编辑的 Cron 配置表。执行 `/cron repair` 后会创建配置表。")
	} else {
		summaryLines = append(summaryLines,
			fmt.Sprintf("实例：%s", firstNonEmpty(strings.TrimSpace(stateValue.InstanceLabel), "unknown")),
			cronBindingLinkLine(stateValue),
			"编辑 `任务配置` 或 `工作区清单` 后，执行 `/cron reload` 生效。",
		)
		if strings.TrimSpace(ownerView.StatusLabel) != "" {
			summaryLines = append(summaryLines, "当前状态："+strings.TrimSpace(ownerView.StatusLabel))
		}
		if strings.TrimSpace(ownerView.NextAction) != "" && ownerView.NeedsRepair {
			summaryLines = append(summaryLines, "下一步："+strings.TrimSpace(ownerView.NextAction))
		}
	}
	if strings.TrimSpace(extraSummary) != "" {
		summaryLines = append(summaryLines, strings.TrimSpace(extraSummary))
	}
	return &control.FeishuDirectCommandCatalog{
		Title:        "Cron 配置",
		Summary:      strings.Join(summaryLines, "\n"),
		Interactive:  false,
		DisplayStyle: control.CommandCatalogDisplayDefault,
	}
}

func cronManualCommandSection() control.CommandCatalogSection {
	return control.CommandCatalogSection{
		Title: "手动输入",
		Entries: []control.CommandCatalogEntry{{
			Commands: []string{
				"/cron",
				"/cron status",
				"/cron list",
				"/cron edit",
				"/cron reload",
				"/cron repair",
			},
			Description: "发送 `/cron` 打开菜单；`/cron status` 查看状态；`/cron list` 查看当前有效任务；`/cron edit` 打开配置表；编辑后执行 `/cron reload` 生效；如需初始化、修 schema 或接管 Cron 配置，执行 `/cron repair`。",
			Form:        control.FeishuCommandFormWithDefault(control.FeishuCommandCron, ""),
		}},
	}
}

func cronBindingLinkLine(stateValue *cronStateFile) string {
	if stateValue == nil || stateValue.Bitable == nil {
		return "配置表：未初始化"
	}
	if url := strings.TrimSpace(stateValue.Bitable.AppURL); url != "" {
		return fmt.Sprintf("配置表：[%s](%s)", "打开 Cron 配置表", url)
	}
	return fmt.Sprintf("配置表：%s", strings.TrimSpace(stateValue.Bitable.AppToken))
}

func cronPrimaryMenuCommand(stateValue *cronStateFile, ownerView cronOwnerView) string {
	if cronRepairShouldBePrimary(stateValue, ownerView) {
		return "/cron repair"
	}
	if cronCanEdit(stateValue) {
		return "/cron edit"
	}
	if cronCanReload(stateValue, ownerView) {
		return "/cron reload"
	}
	return "/cron status"
}

func cronPrimaryDetailCommand(stateValue *cronStateFile, ownerView cronOwnerView) string {
	if cronRepairShouldBePrimary(stateValue, ownerView) {
		return "/cron repair"
	}
	if cronCanEdit(stateValue) {
		return "/cron edit"
	}
	if cronCanReload(stateValue, ownerView) {
		return "/cron reload"
	}
	return ""
}

func cronPrimaryEditCommand(stateValue *cronStateFile, ownerView cronOwnerView) string {
	if cronRepairShouldBePrimary(stateValue, ownerView) {
		return "/cron repair"
	}
	if cronCanReload(stateValue, ownerView) {
		return "/cron reload"
	}
	return ""
}

func cronPrimaryButtonStyle(primaryCommand, commandText string) string {
	if strings.TrimSpace(primaryCommand) == strings.TrimSpace(commandText) {
		return "primary"
	}
	return ""
}

func cronRepairShouldBePrimary(stateValue *cronStateFile, ownerView cronOwnerView) bool {
	if !cronStateHasBinding(stateValue) {
		return true
	}
	return ownerView.NeedsRepair
}

func cronCanEdit(stateValue *cronStateFile) bool {
	return cronStateHasBinding(stateValue)
}

func cronCanReload(stateValue *cronStateFile, ownerView cronOwnerView) bool {
	if !cronStateHasBinding(stateValue) {
		return false
	}
	switch ownerView.Status {
	case cronOwnerStatusHealthy, cronOwnerStatusLegacy:
		return true
	default:
		return false
	}
}

func cronOwnerAllowsLoadedJobs(status cronOwnerStatus) bool {
	switch status {
	case cronOwnerStatusHealthy, cronOwnerStatusLegacy:
		return true
	default:
		return false
	}
}

func cronLoadedJobCountLine(stateValue *cronStateFile, ownerView cronOwnerView) string {
	if stateValue == nil {
		return ""
	}
	if !cronOwnerAllowsLoadedJobs(ownerView.Status) && cronStateHasBinding(stateValue) {
		return "当前任务：待修复后重新加载"
	}
	return fmt.Sprintf("当前已加载任务：%d 条", len(stateValue.Jobs))
}

func cronLoadedJobLines(jobs []cronJobState) []string {
	items := append([]cronJobState(nil), jobs...)
	sort.Slice(items, func(i, j int) bool {
		left := items[i].NextRunAt
		right := items[j].NextRunAt
		switch {
		case left.IsZero() && right.IsZero():
			return firstNonEmpty(items[i].Name, items[i].RecordID) < firstNonEmpty(items[j].Name, items[j].RecordID)
		case left.IsZero():
			return false
		case right.IsZero():
			return true
		case !left.Equal(right):
			return left.Before(right)
		default:
			return firstNonEmpty(items[i].Name, items[i].RecordID) < firstNonEmpty(items[j].Name, items[j].RecordID)
		}
	})
	lines := make([]string, 0, len(items))
	for _, job := range items {
		item := cronReloadTaskItemFromJob(job)
		segments := []string{fmt.Sprintf("`%s`", firstNonEmpty(strings.TrimSpace(job.Name), strings.TrimSpace(job.RecordID), "unnamed"))}
		if schedule := cronReloadTaskScheduleText(item); schedule != "" {
			segments = append(segments, schedule)
		}
		if next := cronReloadTaskNextRunText(item, "下次"); next != "" {
			segments = append(segments, next)
		}
		segments = append(segments, cronJobConcurrencyText(job.MaxConcurrency))
		if source := strings.TrimSpace(cronJobDisplaySource(job)); source != "" {
			segments = append(segments, "来源："+source)
		}
		lines = append(lines, "- "+strings.Join(segments, "｜"))
	}
	return lines
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
