package cronruntime

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

const (
	ScheduleTypeDaily    = "每天定时"
	ScheduleTypeInterval = "间隔运行"
)

type IntervalChoice struct {
	Label   string
	Minutes int
}

var IntervalChoices = []IntervalChoice{
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

type CommandMode string

const (
	CommandModeMenu   CommandMode = "menu"
	CommandModeStatus CommandMode = "status"
	CommandModeList   CommandMode = "list"
	CommandModeRun    CommandMode = "run"
	CommandModeEdit   CommandMode = "edit"
	CommandModeRepair CommandMode = "repair"
	CommandModeReload CommandMode = "reload"
)

type ParsedCommand struct {
	Mode        CommandMode
	JobRecordID string
}

func ParseCommandText(text string) (ParsedCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ParsedCommand{}, fmt.Errorf("缺少 /cron 子命令。")
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 || strings.ToLower(fields[0]) != "/cron" {
		return ParsedCommand{}, fmt.Errorf("不支持的 /cron 子命令。")
	}
	switch len(fields) {
	case 1:
		return ParsedCommand{Mode: CommandModeMenu}, nil
	case 2:
		switch strings.ToLower(fields[1]) {
		case "status":
			return ParsedCommand{Mode: CommandModeStatus}, nil
		case "list":
			return ParsedCommand{Mode: CommandModeList}, nil
		case "edit":
			return ParsedCommand{Mode: CommandModeEdit}, nil
		case "repair":
			return ParsedCommand{Mode: CommandModeRepair}, nil
		case "reload":
			return ParsedCommand{Mode: CommandModeReload}, nil
		}
		return ParsedCommand{}, fmt.Errorf("`/cron` 只支持 `/cron status`、`/cron list`、`/cron edit`、`/cron reload`、`/cron repair`，以及按钮触发的 `/cron run <任务记录ID>`。")
	case 3:
		if strings.ToLower(fields[1]) == "run" {
			jobRecordID := strings.TrimSpace(fields[2])
			if jobRecordID == "" {
				return ParsedCommand{}, fmt.Errorf("`/cron run` 需要任务记录 ID。")
			}
			return ParsedCommand{Mode: CommandModeRun, JobRecordID: jobRecordID}, nil
		}
		return ParsedCommand{}, fmt.Errorf("`/cron` 只支持 `/cron status`、`/cron list`、`/cron edit`、`/cron reload`、`/cron repair`，以及按钮触发的 `/cron run <任务记录ID>`。")
	default:
		return ParsedCommand{}, fmt.Errorf("`/cron status` / `/cron list` / `/cron edit` / `/cron reload` / `/cron repair` 不接受额外参数；单任务触发请使用 `/cron run <任务记录ID>`。")
	}
}

func DailyTimeFromFields(fields map[string]any) (int, int, bool) {
	if len(fields) == 0 {
		return 0, 0, false
	}
	if text := strings.TrimSpace(valueString(fields["调度时间"])); text != "" {
		return parseCronClockText(text)
	}
	hourValue, hourExists := fields["每天-时"]
	minuteValue, minuteExists := fields["每天-分"]
	if !hourExists && !minuteExists {
		return 0, 0, false
	}
	hour := valueInt(hourValue)
	minute := valueInt(minuteValue)
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func parseCronClockText(value string) (int, int, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "：", ":"))
	if value == "" {
		return 0, 0, false
	}
	left, right, ok := strings.Cut(value, ":")
	if !ok {
		return 0, 0, false
	}
	if strings.Contains(strings.TrimSpace(right), ":") {
		return 0, 0, false
	}
	hour, err := strconv.Atoi(strings.TrimSpace(left))
	if err != nil {
		return 0, 0, false
	}
	minute, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil {
		return 0, 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func UsageEvents(surfaceID, formDefault, message string) []eventcontract.Event {
	page := control.FeishuPageViewFromCommandPageView(
		control.NormalizeFeishuPageView(
			BuildRootPageView(nil, OwnerView{}, "", false, formDefault, "error", message),
		),
	)
	return []eventcontract.Event{
		eventcontract.NewEventFromPayload(
			eventcontract.PagePayload{View: page},
			eventcontract.EventMeta{
				Target: eventcontract.TargetRef{
					SurfaceSessionID: strings.TrimSpace(surfaceID),
				},
			},
		),
	}
}

func BindingSummaryLines(stateValue *StateFile, configReady bool) []string {
	if stateValue == nil || stateValue.Bitable == nil {
		return []string{"配置表：未初始化"}
	}
	lines := []string{ConfigSummaryLine(stateValue, configReady)}
	if line := RunsSummaryLine(stateValue, configReady); line != "" {
		lines = append(lines, line)
	}
	return lines
}

func ConfigSummaryLine(stateValue *StateFile, configReady bool) string {
	if stateValue == nil || stateValue.Bitable == nil {
		return "配置表：未初始化"
	}
	if !configReady {
		return "配置表：工作区清单未同步，暂不开放配置入口"
	}
	if value := BitableTableURL(stateValue.Bitable.AppURL, stateValue.Bitable.Tables.Tasks); value != "" {
		return "配置表：可从下方外部入口打开"
	}
	return fmt.Sprintf("配置表：%s", strings.TrimSpace(stateValue.Bitable.AppToken))
}

func RunsSummaryLine(stateValue *StateFile, configReady bool) string {
	if stateValue == nil || stateValue.Bitable == nil || !configReady {
		return ""
	}
	if strings.TrimSpace(stateValue.Bitable.Tables.Runs) == "" {
		return ""
	}
	if value := BitableTableURL(stateValue.Bitable.AppURL, stateValue.Bitable.Tables.Runs); value != "" {
		return "运行状态：可从下方外部入口打开"
	}
	return ""
}

func ExternalLinkSection(stateValue *StateFile, configReady bool) (control.CommandCatalogSection, bool) {
	buttons := ExternalLinkButtons(stateValue, configReady)
	if len(buttons) == 0 {
		return control.CommandCatalogSection{}, false
	}
	return control.CommandCatalogSection{
		Title: "外部入口",
		Entries: []control.CommandCatalogEntry{{
			Buttons: buttons,
		}},
	}, true
}

func ExternalLinkButtons(stateValue *StateFile, configReady bool) []control.CommandCatalogButton {
	if stateValue == nil || stateValue.Bitable == nil || !configReady {
		return nil
	}
	buttons := []control.CommandCatalogButton{}
	if button, ok := ConfigLinkButton(stateValue, configReady); ok {
		buttons = append(buttons, button)
	}
	if button, ok := RunsLinkButton(stateValue, configReady); ok {
		buttons = append(buttons, button)
	}
	return buttons
}

func ConfigLinkButton(stateValue *StateFile, configReady bool) (control.CommandCatalogButton, bool) {
	if stateValue == nil || stateValue.Bitable == nil || !configReady {
		return control.CommandCatalogButton{}, false
	}
	value := BitableTableURL(stateValue.Bitable.AppURL, stateValue.Bitable.Tables.Tasks)
	if value == "" {
		return control.CommandCatalogButton{}, false
	}
	return openURLButton("打开 Cron 配置表", value, "", false), true
}

func RunsLinkButton(stateValue *StateFile, configReady bool) (control.CommandCatalogButton, bool) {
	if stateValue == nil || stateValue.Bitable == nil || !configReady {
		return control.CommandCatalogButton{}, false
	}
	value := BitableTableURL(stateValue.Bitable.AppURL, stateValue.Bitable.Tables.Runs)
	if value == "" {
		return control.CommandCatalogButton{}, false
	}
	return openURLButton("打开运行记录", value, "", false), true
}

func BitableTableURL(appURL, tableID string) string {
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

func PrimaryMenuCommand(stateValue *StateFile, ownerView OwnerView) string {
	if RepairShouldBePrimary(stateValue, ownerView) {
		return "/cron repair"
	}
	if CanEdit(stateValue) {
		return "/cron edit"
	}
	if CanReload(stateValue, ownerView) {
		return "/cron reload"
	}
	return "/cron status"
}

func PrimaryDetailCommand(stateValue *StateFile, ownerView OwnerView) string {
	if RepairShouldBePrimary(stateValue, ownerView) {
		return "/cron repair"
	}
	if CanEdit(stateValue) {
		return "/cron edit"
	}
	if CanReload(stateValue, ownerView) {
		return "/cron reload"
	}
	return ""
}

func PrimaryEditCommand(stateValue *StateFile, ownerView OwnerView) string {
	if RepairShouldBePrimary(stateValue, ownerView) {
		return "/cron repair"
	}
	if CanReload(stateValue, ownerView) {
		return "/cron reload"
	}
	return ""
}

func PrimaryButtonStyle(primaryCommand, commandText string) string {
	if strings.TrimSpace(primaryCommand) == strings.TrimSpace(commandText) {
		return "primary"
	}
	return ""
}

func RepairShouldBePrimary(stateValue *StateFile, ownerView OwnerView) bool {
	if !StateHasBinding(stateValue) {
		return true
	}
	return ownerView.NeedsRepair
}

func CanEdit(stateValue *StateFile) bool {
	return StateHasBinding(stateValue)
}

func CanReload(stateValue *StateFile, ownerView OwnerView) bool {
	if !StateHasBinding(stateValue) {
		return false
	}
	switch ownerView.Status {
	case OwnerStatusHealthy:
		return true
	default:
		return false
	}
}

func OwnerAllowsLoadedJobs(status OwnerStatus) bool {
	switch status {
	case OwnerStatusHealthy:
		return true
	default:
		return false
	}
}

func LoadedJobCountLine(stateValue *StateFile, ownerView OwnerView) string {
	if stateValue == nil {
		return ""
	}
	if !OwnerAllowsLoadedJobs(ownerView.Status) && StateHasBinding(stateValue) {
		return "当前任务：待修复后重新加载"
	}
	return fmt.Sprintf("当前已加载任务：%d 条", len(stateValue.Jobs))
}

func SortedJobs(jobs []JobState) []JobState {
	items := append([]JobState(nil), jobs...)
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

func LoadedJobEntries(jobs []JobState, timeZone string) []control.CommandCatalogEntry {
	items := SortedJobs(jobs)
	entries := make([]control.CommandCatalogEntry, 0, len(items))
	for _, job := range items {
		item := ReloadTaskItemFromJob(job)
		segments := []string{}
		if schedule := ReloadTaskScheduleText(item); schedule != "" {
			segments = append(segments, schedule)
		}
		if next := ReloadTaskNextRunText(item, "下次", timeZone); next != "" {
			segments = append(segments, next)
		}
		segments = append(segments, JobConcurrencyText(job.MaxConcurrency))
		if source := strings.TrimSpace(JobDisplaySource(job)); source != "" {
			segments = append(segments, "来源："+source)
		}
		entry := control.CommandCatalogEntry{
			Title:       firstNonEmpty(strings.TrimSpace(job.Name), strings.TrimSpace(job.RecordID), "unnamed"),
			Description: strings.Join(segments, "｜"),
		}
		if commandText := RunCommandText(job.RecordID); commandText != "" {
			entry.Buttons = []control.CommandCatalogButton{
				runCommandButton("立即触发", commandText, "", false),
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func RunCommandText(jobRecordID string) string {
	jobRecordID = strings.TrimSpace(jobRecordID)
	if jobRecordID == "" {
		return ""
	}
	return "/cron run " + jobRecordID
}

func NoticeEvent(surfaceID, code, text string) eventcontract.Event {
	return eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: "Cron",
			Text:  text,
		},
	}
}
