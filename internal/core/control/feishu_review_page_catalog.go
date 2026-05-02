package control

import frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"

func BuildFeishuReviewRootPageView(inMenu bool) FeishuPageView {
	return NormalizeFeishuPageView(FeishuPageView{
		CommandID:    FeishuCommandReview,
		Title:        "审阅代码变更",
		Interactive:  true,
		DisplayStyle: CommandCatalogDisplayCompactButtons,
		Breadcrumbs:  reviewPageBreadcrumbs(inMenu),
		Sections: []CommandCatalogSection{{
			Entries: []CommandCatalogEntry{
				reviewRootPageEntry(
					"Review 待提交内容",
					"对当前会话所在 Git 工作区的未提交修改发起独立审阅。",
					CommandCatalogButton{
						Label:         "Review 待提交内容",
						Kind:          CommandCatalogButtonCallbackAction,
						CommandID:     FeishuCommandReview,
						CallbackValue: frontstagecontract.ActionPayloadPageAction(string(ActionReviewStartUncommitted), ""),
					},
				),
				reviewRootPageEntry(
					"Review 指定提交",
					"选择最近的提交记录，并对指定 commit 发起独立审阅。",
					CommandCatalogButton{
						Label:         "Review 指定提交",
						Kind:          CommandCatalogButtonCallbackAction,
						CommandID:     FeishuCommandReview,
						CallbackValue: frontstagecontract.ActionPayloadPageAction(string(ActionReviewOpenCommitPicker), ""),
					},
				),
			},
		}},
		RelatedButtons: reviewRootRelatedButtons(inMenu),
	})
}

func reviewRootPageEntry(title, description string, button CommandCatalogButton) CommandCatalogEntry {
	return CommandCatalogEntry{
		Title:       title,
		Description: description,
		Buttons:     []CommandCatalogButton{button},
	}
}

func reviewPageBreadcrumbs(inMenu bool) []CommandCatalogBreadcrumb {
	if inMenu {
		return FeishuCommandBreadcrumbs(FeishuCommandGroupCommonTools, "审阅代码变更")
	}
	return []CommandCatalogBreadcrumb{{Label: "审阅代码变更"}}
}

func reviewRootRelatedButtons(inMenu bool) []CommandCatalogButton {
	if !inMenu {
		return nil
	}
	return FeishuCommandBackButtons(FeishuCommandGroupCommonTools)
}
