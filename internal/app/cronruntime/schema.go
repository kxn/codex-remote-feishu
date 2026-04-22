package cronruntime

import (
	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
	"strings"
)

type FieldSpec struct {
	Name     string
	Type     int
	Property *larkbitable.AppTableFieldProperty
}

func TaskFieldSpecs(workspacesTableID string) []FieldSpec {
	return []FieldSpec{
		{Name: "启用", Type: 7},
		{Name: TaskSourceTypeField, Type: 3, Property: SelectFieldProperty([]string{TaskSourceWorkspaceText, TaskSourceGitRepoText})},
		{Name: TaskWorkspaceField, Type: 18, Property: larkbitable.NewAppTableFieldPropertyBuilder().TableId(workspacesTableID).Multiple(false).Build()},
		{Name: TaskGitRepoInputField, Type: 1},
		{Name: "提示词", Type: 1},
		{Name: "调度类型", Type: 3, Property: SelectFieldProperty([]string{ScheduleTypeDaily, ScheduleTypeInterval})},
		{Name: "调度时间", Type: 1},
		{Name: "间隔", Type: 3, Property: SelectFieldProperty(IntervalLabels())},
		{Name: TaskConcurrencyField, Type: 2},
		{Name: "超时（分钟）", Type: 2},
		{Name: "最近运行时间", Type: 5, Property: DateTimeFieldProperty()},
		{Name: "最近状态", Type: 1},
		{Name: "最近结果摘要", Type: 1},
		{Name: "最近错误", Type: 1},
	}
}

func FieldTypeMatches(spec FieldSpec, current int) bool {
	if current == spec.Type {
		return true
	}
	return spec.Name == "启用" && spec.Type == 7 && current == 3
}

func PermissionSatisfies(current, desired string) bool {
	return PermissionRank(current) >= PermissionRank(desired)
}

func PermissionRank(value string) int {
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

func SelectFieldProperty(labels []string) *larkbitable.AppTableFieldProperty {
	options := make([]*larkbitable.AppTableFieldPropertyOption, 0, len(labels))
	for index, label := range labels {
		options = append(options, larkbitable.NewAppTableFieldPropertyOptionBuilder().
			Name(label).
			Color(index%54).
			Build())
	}
	return larkbitable.NewAppTableFieldPropertyBuilder().
		Options(options).
		Build()
}

func DateTimeFieldProperty() *larkbitable.AppTableFieldProperty {
	return larkbitable.NewAppTableFieldPropertyBuilder().
		DateFormatter("yyyy/MM/dd HH:mm").
		AutoFill(false).
		Build()
}

func IntervalLabels() []string {
	values := make([]string, 0, len(IntervalChoices))
	for _, item := range IntervalChoices {
		values = append(values, item.Label)
	}
	return values
}
