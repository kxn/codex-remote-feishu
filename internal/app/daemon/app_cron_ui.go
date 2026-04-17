package daemon

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

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
	cronCommandRun    cronCommandMode = "run"
	cronCommandEdit   cronCommandMode = "edit"
	cronCommandRepair cronCommandMode = "repair"
	cronCommandReload cronCommandMode = "reload"
)

type parsedCronCommand struct {
	Mode        cronCommandMode
	JobRecordID string
}

func parseCronCommandText(text string) (parsedCronCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return parsedCronCommand{}, fmt.Errorf("缺少 /cron 子命令。")
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 || strings.ToLower(fields[0]) != "/cron" {
		return parsedCronCommand{}, fmt.Errorf("不支持的 /cron 子命令。")
	}
	switch len(fields) {
	case 1:
		return parsedCronCommand{Mode: cronCommandMenu}, nil
	case 2:
		switch strings.ToLower(fields[1]) {
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
		return parsedCronCommand{}, fmt.Errorf("`/cron` 只支持 `/cron status`、`/cron list`、`/cron edit`、`/cron reload`、`/cron repair`，以及按钮触发的 `/cron run <任务记录ID>`。")
	case 3:
		if strings.ToLower(fields[1]) == "run" {
			jobRecordID := strings.TrimSpace(fields[2])
			if jobRecordID == "" {
				return parsedCronCommand{}, fmt.Errorf("`/cron run` 需要任务记录 ID。")
			}
			return parsedCronCommand{Mode: cronCommandRun, JobRecordID: jobRecordID}, nil
		}
		return parsedCronCommand{}, fmt.Errorf("`/cron` 只支持 `/cron status`、`/cron list`、`/cron edit`、`/cron reload`、`/cron repair`，以及按钮触发的 `/cron run <任务记录ID>`。")
	default:
		return parsedCronCommand{}, fmt.Errorf("`/cron status` / `/cron list` / `/cron edit` / `/cron reload` / `/cron repair` 不接受额外参数；单任务触发请使用 `/cron run <任务记录ID>`。")
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
		FeishuDirectCommandCatalog: buildCronMenuCatalog(nil, cronOwnerView{}, "", false),
	})
	return events
}

func buildCronMenuCatalog(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string, configReady bool) *control.FeishuDirectCommandCatalog {
	summaryLines := []string{"选择 Cron 的下一步操作。"}
	if strings.TrimSpace(ownerView.StatusLabel) != "" {
		summaryLines = append(summaryLines, "当前状态："+strings.TrimSpace(ownerView.StatusLabel))
	}
	if stateValue != nil {
		summaryLines = append(summaryLines, fmt.Sprintf("实例：%s", firstNonEmpty(strings.TrimSpace(stateValue.InstanceLabel), "unknown")))
		if line := cronLoadedJobCountLine(stateValue, ownerView); line != "" {
			summaryLines = append(summaryLines, line)
		}
		if cronStateHasBinding(stateValue) && !configReady {
			summaryLines = append(summaryLines, "配置入口：工作区清单未同步，暂不可用。")
		}
	}
	if strings.TrimSpace(ownerView.NextAction) != "" {
		summaryLines = append(summaryLines, "下一步："+strings.TrimSpace(ownerView.NextAction))
	}
	if strings.TrimSpace(extraSummary) != "" {
		summaryLines = append(summaryLines, strings.TrimSpace(extraSummary))
	}
	primaryCommand := cronPrimaryMenuCommand(stateValue, ownerView)
	canEdit := cronCanEdit(stateValue) && configReady
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

func buildCronStatusCatalog(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string, configReady bool) *control.FeishuDirectCommandCatalog {
	summaryLines := []string{}
	cronZone := cronConfiguredTimeZone(stateValue)
	if stateValue == nil || !cronStateHasBinding(stateValue) {
		summaryLines = append(summaryLines, "当前实例还没有初始化 Cron 配置表。执行 `/cron repair` 后会创建配置表。")
	} else {
		summaryLines = append(summaryLines,
			fmt.Sprintf("实例：%s", firstNonEmpty(strings.TrimSpace(stateValue.InstanceLabel), "unknown")),
		)
		summaryLines = append(summaryLines, cronBindingLinkLines(stateValue, configReady)...)
		if line := cronLoadedJobCountLine(stateValue, ownerView); line != "" {
			summaryLines = append(summaryLines, line)
		}
		if !stateValue.LastWorkspaceSyncAt.IsZero() {
			summaryLines = append(summaryLines, fmt.Sprintf("最近工作区同步：%s", cronFormatDisplayTime(stateValue.LastWorkspaceSyncAt, cronZone)))
		}
		if !stateValue.LastReloadAt.IsZero() {
			summaryLines = append(summaryLines, fmt.Sprintf("最近 reload：%s", cronFormatDisplayTime(stateValue.LastReloadAt, cronZone)))
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
	cronZone := cronConfiguredTimeZone(stateValue)
	sections := []control.CommandCatalogSection{}
	interactive := false
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
		summaryLines = append(summaryLines,
			fmt.Sprintf("当前已加载 %d 条任务。", len(stateValue.Jobs)),
			"每条任务都会显示下一次执行时间；点击 `立即触发` 可手动执行一次，不影响原有下次调度时间。",
		)
		entries := cronLoadedJobEntries(stateValue.Jobs, cronZone)
		if len(entries) != 0 {
			interactive = true
			sections = append(sections, control.CommandCatalogSection{
				Title:   "已加载任务",
				Entries: entries,
			})
		}
	}
	if strings.TrimSpace(extraSummary) != "" {
		summaryLines = append(summaryLines, "", strings.TrimSpace(extraSummary))
	}
	return &control.FeishuDirectCommandCatalog{
		Title:        "Cron 任务",
		Summary:      strings.Join(summaryLines, "\n"),
		Interactive:  interactive,
		DisplayStyle: control.CommandCatalogDisplayDefault,
		Sections:     sections,
	}
}

func buildCronEditCatalog(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string, configReady bool) *control.FeishuDirectCommandCatalog {
	summaryLines := []string{}
	if stateValue == nil || !cronStateHasBinding(stateValue) {
		summaryLines = append(summaryLines, "当前还没有可编辑的 Cron 配置表。执行 `/cron repair` 后会创建配置表。")
	} else {
		summaryLines = append(summaryLines,
			fmt.Sprintf("实例：%s", firstNonEmpty(strings.TrimSpace(stateValue.InstanceLabel), "unknown")),
		)
		summaryLines = append(summaryLines, cronConfigLinkLine(stateValue, configReady))
		if configReady {
			summaryLines = append(summaryLines, "编辑 `任务配置` 或 `工作区清单` 后，执行 `/cron reload` 生效。")
		} else {
			summaryLines = append(summaryLines, "工作区清单同步完成后才会开放配置入口；如需立即修复可执行 `/cron repair`。")
		}
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
			Description: "发送 `/cron` 打开菜单；`/cron status` 查看状态；`/cron list` 查看当前有效任务并可按钮立即触发；`/cron edit` 打开配置表；编辑后执行 `/cron reload` 生效；如需初始化、修 schema 或接管 Cron 配置，执行 `/cron repair`。",
			Form:        control.FeishuCommandFormWithDefault(control.FeishuCommandCron, ""),
		}},
	}
}

func cronBindingLinkLines(stateValue *cronStateFile, configReady bool) []string {
	if stateValue == nil || stateValue.Bitable == nil {
		return []string{"配置表：未初始化"}
	}
	lines := []string{cronConfigLinkLine(stateValue, configReady)}
	if line := cronRunsLinkLine(stateValue); line != "" {
		if configReady {
			lines = append(lines, line)
		}
	}
	return lines
}

func cronConfigLinkLine(stateValue *cronStateFile, configReady bool) string {
	if stateValue == nil || stateValue.Bitable == nil {
		return "配置表：未初始化"
	}
	if !configReady {
		return "配置表：工作区清单未同步，暂不开放配置入口"
	}
	if url := cronBitableTableURL(stateValue.Bitable.AppURL, stateValue.Bitable.Tables.Tasks); url != "" {
		return fmt.Sprintf("配置表：[%s](%s)", "打开 Cron 配置表", url)
	}
	return fmt.Sprintf("配置表：%s", strings.TrimSpace(stateValue.Bitable.AppToken))
}

func cronRunsLinkLine(stateValue *cronStateFile) string {
	if stateValue == nil || stateValue.Bitable == nil {
		return ""
	}
	if strings.TrimSpace(stateValue.Bitable.Tables.Runs) == "" {
		return ""
	}
	if url := cronBitableTableURL(stateValue.Bitable.AppURL, stateValue.Bitable.Tables.Runs); url != "" {
		return fmt.Sprintf("运行状态：[%s](%s)", "打开运行记录", url)
	}
	return ""
}

func cronBitableTableURL(appURL, tableID string) string {
	appURL = strings.TrimSpace(appURL)
	if appURL == "" {
		return ""
	}
	if strings.TrimSpace(tableID) == "" {
		return appURL
	}
	parsed, err := url.Parse(appURL)
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return appURL
	}
	query := parsed.Query()
	query.Set("table", strings.TrimSpace(tableID))
	query.Del("view")
	query.Del("record")
	query.Del("field")
	query.Del("search")
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
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

func cronSortedJobs(jobs []cronJobState) []cronJobState {
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
	return items
}

func cronLoadedJobEntries(jobs []cronJobState, timeZone string) []control.CommandCatalogEntry {
	items := cronSortedJobs(jobs)
	entries := make([]control.CommandCatalogEntry, 0, len(items))
	for _, job := range items {
		item := cronReloadTaskItemFromJob(job)
		segments := []string{}
		if schedule := cronReloadTaskScheduleText(item); schedule != "" {
			segments = append(segments, schedule)
		}
		if next := cronReloadTaskNextRunText(item, "下次", timeZone); next != "" {
			segments = append(segments, next)
		}
		segments = append(segments, cronJobConcurrencyText(job.MaxConcurrency))
		if source := strings.TrimSpace(cronJobDisplaySource(job)); source != "" {
			segments = append(segments, "来源："+source)
		}
		entry := control.CommandCatalogEntry{
			Title:       firstNonEmpty(strings.TrimSpace(job.Name), strings.TrimSpace(job.RecordID), "unnamed"),
			Description: strings.Join(segments, "｜"),
		}
		if commandText := cronRunCommandText(job.RecordID); commandText != "" {
			entry.Buttons = []control.CommandCatalogButton{
				runCommandButton("立即触发", commandText, "", false),
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func cronRunCommandText(jobRecordID string) string {
	jobRecordID = strings.TrimSpace(jobRecordID)
	if jobRecordID == "" {
		return ""
	}
	return "/cron run " + jobRecordID
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
