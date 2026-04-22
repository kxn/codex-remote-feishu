package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func buildCronRootPageView(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string, configReady bool, formDefault, statusKind, statusText string) control.FeishuPageView {
	primaryCommand := cronPrimaryMenuCommand(stateValue, ownerView)
	canEdit := cronCanEdit(stateValue) && configReady
	canReload := cronCanReload(stateValue, ownerView)
	var summarySections []control.FeishuCardTextSection
	if line := strings.TrimSpace(extraSummary); line != "" {
		summarySections = commandCatalogSummarySections(line)
	}
	return control.FeishuPageView{
		CommandID:       control.FeishuCommandCron,
		SummarySections: summarySections,
		StatusKind:      strings.TrimSpace(statusKind),
		StatusText:      strings.TrimSpace(statusText),
		Interactive:     true,
		DisplayStyle:    control.CommandCatalogDisplayCompactButtons,
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
		},
	}
}

func buildCronStatusPageView(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string, configReady bool) control.FeishuPageView {
	summaryLines := []string{}
	cronZone := cronConfiguredTimeZone(stateValue)
	sections := []control.CommandCatalogSection{}
	if stateValue == nil || !cronStateHasBinding(stateValue) {
		summaryLines = append(summaryLines, "当前实例还没有初始化 Cron 配置表。执行 /cron repair 后会创建配置表。")
	} else {
		summaryLines = append(summaryLines,
			fmt.Sprintf("实例：%s", firstNonEmpty(strings.TrimSpace(stateValue.InstanceLabel), "unknown")),
		)
		summaryLines = append(summaryLines, cronBindingSummaryLines(stateValue, configReady)...)
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
		if section, ok := cronExternalLinkSection(stateValue, configReady); ok {
			sections = append(sections, section)
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
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		CommandID:       control.FeishuCommandCron,
		Title:           "Cron 状态",
		Breadcrumbs:     control.FeishuCommandBreadcrumbsForCommand(control.FeishuCommandCron, "当前状态"),
		SummarySections: commandCatalogSummarySections(summaryLines...),
		Interactive:     len(sections) != 0,
		DisplayStyle:    control.CommandCatalogDisplayDefault,
		Sections:        sections,
		RelatedButtons:  control.FeishuCommandBackToRootButtons(control.FeishuCommandCron),
	})
}

func buildCronListPageView(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string) control.FeishuPageView {
	summaryLines := []string{}
	cronZone := cronConfiguredTimeZone(stateValue)
	sections := []control.CommandCatalogSection{}
	interactive := false
	switch {
	case stateValue == nil || !cronStateHasBinding(stateValue):
		summaryLines = append(summaryLines, "当前实例还没有初始化 Cron 配置表。执行 /cron repair 后会创建配置表。")
	case !cronOwnerAllowsLoadedJobs(ownerView.Status):
		summaryLines = append(summaryLines,
			"当前 Cron 绑定需要先修复后才能确认有效任务。",
			"执行 /cron repair 完成接管后，再执行 /cron reload 重新加载任务。",
		)
	case len(stateValue.Jobs) == 0:
		summaryLines = append(summaryLines, "当前没有已加载的 Cron 任务。编辑表格后执行 /cron reload 生效。")
	default:
		summaryLines = append(summaryLines,
			fmt.Sprintf("当前已加载 %d 条任务。", len(stateValue.Jobs)),
			"每条任务都会显示下一次执行时间；点击立即触发可手动执行一次，不影响原有下次调度时间。",
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
		summaryLines = append(summaryLines, strings.TrimSpace(extraSummary))
	}
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		CommandID:       control.FeishuCommandCron,
		Title:           "Cron 任务",
		Breadcrumbs:     control.FeishuCommandBreadcrumbsForCommand(control.FeishuCommandCron, "当前任务"),
		SummarySections: commandCatalogSummarySections(summaryLines...),
		Interactive:     interactive,
		DisplayStyle:    control.CommandCatalogDisplayDefault,
		Sections:        sections,
		RelatedButtons:  control.FeishuCommandBackToRootButtons(control.FeishuCommandCron),
	})
}

func buildCronEditPageView(stateValue *cronStateFile, ownerView cronOwnerView, extraSummary string, configReady bool) control.FeishuPageView {
	summaryLines := []string{}
	sections := []control.CommandCatalogSection{}
	if stateValue == nil || !cronStateHasBinding(stateValue) {
		summaryLines = append(summaryLines, "当前还没有可编辑的 Cron 配置表。执行 /cron repair 后会创建配置表。")
	} else {
		summaryLines = append(summaryLines,
			fmt.Sprintf("实例：%s", firstNonEmpty(strings.TrimSpace(stateValue.InstanceLabel), "unknown")),
		)
		summaryLines = append(summaryLines, cronConfigSummaryLine(stateValue, configReady))
		if configReady {
			summaryLines = append(summaryLines, "编辑任务配置或工作区清单后，执行 /cron reload 生效。")
			if button, ok := cronConfigLinkButton(stateValue, configReady); ok {
				sections = append(sections, control.CommandCatalogSection{
					Title: "外部入口",
					Entries: []control.CommandCatalogEntry{{
						Buttons: []control.CommandCatalogButton{button},
					}},
				})
			}
		} else {
			summaryLines = append(summaryLines, "工作区清单同步完成后才会开放配置入口；如需立即修复可执行 /cron repair。")
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
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		CommandID:       control.FeishuCommandCron,
		Title:           "Cron 配置",
		Breadcrumbs:     control.FeishuCommandBreadcrumbsForCommand(control.FeishuCommandCron, "打开配置"),
		SummarySections: commandCatalogSummarySections(summaryLines...),
		Interactive:     len(sections) != 0,
		DisplayStyle:    control.CommandCatalogDisplayDefault,
		Sections:        sections,
		RelatedButtons:  control.FeishuCommandBackToRootButtons(control.FeishuCommandCron),
	})
}
