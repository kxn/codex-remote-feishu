package control

import "strings"

func goalPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandGoal)
	if strings.TrimSpace(view.StatusKind) == "error" {
		return FeishuPageView{
			PageID:     def.ID,
			CommandID:  def.ID,
			Title:      def.Title,
			StatusKind: "error",
			StatusText: strings.TrimSpace(view.StatusText),
			Sealed:     true,
		}
	}
	return commandConfigPageView(def, view, nil, nil, []CommandCatalogSection{{
		Title: "Goal",
		Entries: []CommandCatalogEntry{{
			Description: "正在查询当前会话 Goal 状态…",
		}},
	}})
}
