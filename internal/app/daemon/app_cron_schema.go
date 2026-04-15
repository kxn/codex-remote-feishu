package daemon

import (
	"strings"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

func cronTaskFieldSpecs(workspacesTableID string) []cronFieldSpec {
	return []cronFieldSpec{
		{Name: "启用", Type: 7},
		{Name: "工作区", Type: 18, Property: larkbitable.NewAppTableFieldPropertyBuilder().TableId(workspacesTableID).Multiple(false).Build()},
		{Name: "提示词", Type: 1},
		{Name: "调度类型", Type: 3, Property: cronSelectFieldProperty([]string{cronScheduleTypeDaily, cronScheduleTypeInterval})},
		{Name: "调度时间", Type: 1},
		{Name: "间隔", Type: 3, Property: cronSelectFieldProperty(cronIntervalLabels())},
		{Name: "超时（分钟）", Type: 2},
		{Name: "最近运行时间", Type: 5},
		{Name: "最近状态", Type: 1},
		{Name: "最近结果摘要", Type: 1},
		{Name: "最近错误", Type: 1},
	}
}

func cronFieldTypeMatches(spec cronFieldSpec, current int) bool {
	if current == spec.Type {
		return true
	}
	// Keep old select-based enable fields readable after switching new tables to checkbox.
	return spec.Name == "启用" && spec.Type == 7 && current == 3
}

func cronPermissionSatisfies(current, desired string) bool {
	return cronPermissionRank(current) >= cronPermissionRank(desired)
}

func cronPermissionRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "view":
		return 10
	case "comment":
		return 20
	case "edit":
		return 30
	case "full_access":
		return 40
	default:
		return 0
	}
}

func cronSelectFieldProperty(labels []string) *larkbitable.AppTableFieldProperty {
	options := make([]*larkbitable.AppTableFieldPropertyOption, 0, len(labels))
	for index, label := range labels {
		options = append(options, larkbitable.NewAppTableFieldPropertyOptionBuilder().
			Name(label).
			Color(index%54).
			Build())
	}
	return larkbitable.NewAppTableFieldPropertyBuilder().Options(options).Build()
}

func cronIntervalLabels() []string {
	values := make([]string, 0, len(cronIntervalChoices))
	for _, item := range cronIntervalChoices {
		values = append(values, item.Label)
	}
	return values
}
