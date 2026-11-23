package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func predictionPayload(p *predictionRecord, submit bool) map[string]any {
	var value map[string]any
	if submit {
		value = frontstagecontract.ActionPayloadPageLocalSubmit(string(control.ActionPredictionSend), "", "prediction_text")
	} else {
		value = frontstagecontract.ActionPayloadPageLocalAction(string(control.ActionPredictionAccept), "")
	}
	value[frontstagecontract.CardActionPayloadKeyRequestID] = p.ID
	return value
}

func predictionSuggestionPage(p *predictionRecord) control.FeishuPageView {
	return control.FeishuPageView{PageID: "prediction", Title: "下一句建议", Interactive: true,
		StatusText:      "可采用后编辑，发送前不会开始新任务。",
		SummarySections: []control.FeishuCardTextSection{{Lines: []string{p.Suggestion}}},
		RelatedButtons: []control.CommandCatalogButton{{Label: "采用并编辑", Kind: control.CommandCatalogButtonCallbackAction,
			CallbackValue: predictionPayload(p, false)}},
		SuppressDefaultRelatedButtons: true}
}

func predictionDraftPage(p *predictionRecord) control.FeishuPageView {
	text := p.Suggestion
	if p.Draft {
		text = p.EditedText
	}
	return control.FeishuPageView{PageID: "prediction", Title: "编辑下一条消息", Interactive: true, MessageID: p.MessageID,
		StatusText: "确认内容后点击发送。",
		Sections: []control.CommandCatalogSection{{Entries: []control.CommandCatalogEntry{{Form: &control.CommandCatalogForm{
			SubmitLabel: "发送", SubmitValue: predictionPayload(p, true),
			Field: control.CommandCatalogFormField{Name: "prediction_text", Kind: control.CommandCatalogFormFieldMultilineText,
				Label: "消息内容", DefaultValue: text},
		}}}}}, SuppressDefaultRelatedButtons: true}
}

func predictionDraftCallbackPage(p *predictionRecord) control.FeishuPageView {
	view := predictionDraftPage(p)
	// Replace through the callback; retain the record message ID for asynchronous failures.
	view.MessageID = ""
	return view
}

func predictionSubmittedPage(p *predictionRecord, started bool) control.FeishuPageView {
	status := "已提交，等待处理。"
	if started {
		status = "已发送，任务已开始处理。"
	}
	return control.FeishuPageView{PageID: "prediction", Title: "已提交的消息", MessageID: p.MessageID,
		StatusText:                    status,
		SummarySections:               []control.FeishuCardTextSection{{Lines: []string{p.EditedText}}},
		SuppressDefaultRelatedButtons: true}
}
