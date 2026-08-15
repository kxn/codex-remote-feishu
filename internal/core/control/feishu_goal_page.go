package control

import "strings"

func goalPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandGoal)
	if strings.TrimSpace(view.StatusKind) == "error" {
		return commandConfigPageView(
			def,
			view,
			BuildFeishuCommandConfigBodySections(def, view),
			BuildFeishuCommandConfigNoticeSections(def, view),
			nil,
		)
	}
	return commandConfigPageView(def, view, nil, nil, []CommandCatalogSection{{
		Title: "Goal",
		Entries: []CommandCatalogEntry{{
			Description: "正在查询当前会话 Goal 状态…",
		}},
	}})
}
