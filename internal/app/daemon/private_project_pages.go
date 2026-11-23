package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func projectButton(label, args string) control.CommandCatalogButton {
	return control.CommandCatalogButton{Label: label, Kind: control.CommandCatalogButtonCallbackAction, CallbackValue: frontstagecontract.ActionPayloadPageLocalAction(string(control.ActionPrivateProject), args)}
}
func projectPage(title string, lines ...string) control.FeishuPageView {
	return control.FeishuPageView{CommandID: control.FeishuCommandProject, Title: title, Interactive: true, Patchable: true, BodySections: []control.FeishuCardTextSection{{Lines: lines}}, SuppressDefaultRelatedButtons: true}
}
func projectPageEvent(action control.Action, page control.FeishuPageView) eventcontract.Event {
	meta := eventcontract.EventMeta{Target: eventcontract.TargetRef{GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID}}
	if action.Inbound != nil && action.Inbound.CardCallback && action.Inbound.OpenMessageID != "" {
		meta.SourceMessageID = action.Inbound.OpenMessageID
		meta.InlineReplaceMode = eventcontract.InlineReplaceCurrentCard
		page.MessageID = meta.SourceMessageID
	}
	return eventcontract.NewEventFromPayload(eventcontract.PagePayload{View: page}, meta)
}
func (r privateHostReducer) projectAction(action control.Action) ([]eventcontract.Event, error) {
	args := strings.Fields(action.Text)
	if len(args) == 0 || args[0] != "/project" {
		return nil, privatechat.ErrJournalState
	}
	kind := control.DaemonCommandPrivateProjectQuery
	if len(args) > 1 && args[1] == "confirm" {
		if action.Inbound == nil || !action.Inbound.CardCallback || action.Inbound.OpenMessageID == "" {
			return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, Notice: &control.Notice{Code: "project_confirm_required", Title: "分享未提交", Text: "请打开分享预览卡，由本人确认。"}}}, nil
		}
		kind = control.DaemonCommandPrivateProjectShare
	}
	raw, e := json.Marshal(action)
	if e != nil {
		return nil, e
	}
	command := eventcontract.Event{Kind: eventcontract.KindDaemonCommand, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, DaemonCommand: &control.DaemonCommand{Kind: kind, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, Text: string(raw)}}
	if action.Inbound != nil && action.Inbound.CardCallback && action.Inbound.OpenMessageID != "" {
		loading := projectPage("项目资料", "正在核对当前权限…")
		return []eventcontract.Event{projectPageEvent(action, loading), command}, nil
	}
	return []eventcontract.Event{command}, nil
}

func (f privateProjectFacade) renderPage(ctx context.Context, a privateProjectAuthority, action control.Action, operationID string) (control.FeishuPageView, error) {
	args := strings.Fields(action.Text)
	if len(args) == 0 || args[0] != "/project" {
		return control.FeishuPageView{}, fmt.Errorf("invalid project action")
	}
	if len(args) == 1 {
		page, e := f.projects(ctx, a, "", 20)
		if e != nil {
			return pageUnavailable(), nil
		}
		view := projectPage("项目资料与分享", "只显示本人当前有权访问且此助手可参与的项目。查看不会切换私人会话。")
		for _, p := range page.Items {
			view.Sections = append(view.Sections, control.CommandCatalogSection{Entries: []control.CommandCatalogEntry{{Title: p.Name, Buttons: []control.CommandCatalogButton{projectButton("查看资料", "resources "+p.ID)}}}})
		}
		view.RelatedButtons = append(view.RelatedButtons, projectButton("待确认分享", "drafts"))
		if page.NextAfter != "" {
			view.RelatedButtons = append(view.RelatedButtons, projectButton("下一页", "projects "+page.NextAfter))
		}
		if len(page.Items) == 0 {
			view.NoticeSections = []control.FeishuCardTextSection{{Lines: []string{"没有找到当前身份可验证访问的项目。请确认本人是项目群成员、此助手已加入该项目，然后重试。"}}}
		}
		return view, nil
	}
	switch args[1] {
	case "projects":
		if len(args) != 3 {
			break
		}
		page, e := f.projects(ctx, a, args[2], 20)
		if e != nil {
			return pageUnavailable(), nil
		}
		view := projectPage("项目资料与分享")
		for _, p := range page.Items {
			view.Sections = append(view.Sections, control.CommandCatalogSection{Entries: []control.CommandCatalogEntry{{Title: p.Name, Buttons: []control.CommandCatalogButton{projectButton("查看资料", "resources "+p.ID)}}}})
		}
		if page.NextAfter != "" {
			view.RelatedButtons = append(view.RelatedButtons, projectButton("下一页", "projects "+page.NextAfter))
		}
		view.RelatedButtons = append(view.RelatedButtons, projectButton("返回", ""))
		return view, nil
	case "resources":
		if len(args) < 3 || len(args) > 4 {
			break
		}
		after := ""
		if len(args) == 4 {
			after = args[3]
		}
		page, e := f.resources(ctx, a, args[2], after, 20)
		if e != nil {
			return pageUnavailable(), nil
		}
		view := projectPage(page.Project.Name, "请选择已登记资料；查看不会改变私人工作区。")
		for _, item := range page.Items {
			view.Sections = append(view.Sections, control.CommandCatalogSection{Entries: []control.CommandCatalogEntry{{Title: item.Purpose, Buttons: []control.CommandCatalogButton{projectButton("查看保存版本", "versions "+page.Project.ID+" "+item.ID)}}}})
		}
		view.Sections = append(view.Sections, control.CommandCatalogSection{Title: "分享指定文字", Entries: []control.CommandCatalogEntry{{Title: "预览后由本人确认", Form: &control.CommandCatalogForm{CommandID: control.FeishuCommandProject, CommandText: "/project share " + page.Project.ID, SubmitLabel: "预览分享", Field: control.CommandCatalogFormField{Name: "text", Kind: control.CommandCatalogFormFieldText, Label: "要分享的文字"}}}}})
		if page.NextAfter != "" {
			view.RelatedButtons = append(view.RelatedButtons, projectButton("下一页", "resources "+page.Project.ID+" "+page.NextAfter))
		}
		view.RelatedButtons = append(view.RelatedButtons, projectButton("返回项目", ""))
		return view, nil
	case "versions":
		if len(args) < 4 || len(args) > 5 {
			break
		}
		after := ""
		if len(args) == 5 {
			after = args[4]
		}
		items, next, e := f.versions(ctx, a, args[2], args[3], after, 20)
		if e != nil {
			return pageUnavailable(), nil
		}
		view := projectPage("资料保存版本", "请选择固定版本；不自动读取未保存的当前原件。")
		for _, v := range items {
			label := "查看版本"
			if !v.Saved {
				label = "未保存"
			}
			view.Sections = append(view.Sections, control.CommandCatalogSection{Entries: []control.CommandCatalogEntry{{Title: label, Buttons: []control.CommandCatalogButton{projectButton("查看", "read "+args[2]+" "+args[3]+" "+v.VersionID)}}}})
		}
		if next != "" {
			view.RelatedButtons = append(view.RelatedButtons, projectButton("下一页", "versions "+args[2]+" "+args[3]+" "+next))
		}
		view.RelatedButtons = append(view.RelatedButtons, projectButton("返回资料", "resources "+args[2]))
		return view, nil
	case "read":
		if len(args) != 5 {
			break
		}
		v, e := f.readVersion(ctx, a, args[2], args[3], args[4])
		if e != nil {
			return pageUnavailable(), nil
		}
		body := v.Content
		if !v.Saved {
			body = "此版本尚未保存。"
		}
		view := projectPage("资料版本", body)
		if v.Saved && v.ContentHash != "" {
			view.RelatedButtons = append(view.RelatedButtons, projectButton("预览分享此版本", "prepare_resource "+args[2]+" "+args[3]+" "+args[4]))
		}
		view.RelatedButtons = append(view.RelatedButtons, projectButton("返回版本", "versions "+args[2]+" "+args[3]))
		return view, nil
	case "prepare_resource":
		if len(args) != 5 {
			break
		}
		v, e := f.readVersion(ctx, a, args[2], args[3], args[4])
		if e != nil || !v.Saved || v.ContentHash == "" {
			return pageUnavailable(), nil
		}
		draft, e := f.journal.PrepareProjectShare(ctx, a.Owner, a.Epoch, "human:"+operationID, privatechat.ProjectShareDraft{ProjectID: args[2], ResourceID: args[3], VersionID: args[4]})
		if e != nil {
			return pageUnavailable(), nil
		}
		return f.renderDraft(ctx, a, draft.ID)
	case "drafts":
		if len(args) > 3 {
			break
		}
		after := ""
		if len(args) == 3 {
			after = args[2]
		}
		drafts, next, e := f.journal.ListProjectShareDrafts(ctx, a.Owner, a.Epoch, after, 20)
		if e != nil {
			return pageUnavailable(), nil
		}
		view := projectPage("待确认分享", "只有本人确认卡片后才会分享。")
		for _, d := range drafts {
			var projectName string
			e = f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
				p, x := f.authorizedProject(tx, a, d.ProjectID)
				if x == nil {
					projectName = p.Name
				}
				return x
			})
			if e != nil {
				continue
			}
			view.Sections = append(view.Sections, control.CommandCatalogSection{Entries: []control.CommandCatalogEntry{{Title: projectName, Buttons: []control.CommandCatalogButton{projectButton("查看并确认", "draft "+d.ID)}}}})
		}
		if next != "" {
			view.RelatedButtons = append(view.RelatedButtons, projectButton("下一页", "drafts "+next))
		}
		view.RelatedButtons = append(view.RelatedButtons, projectButton("返回项目", ""))
		return view, nil
	case "share":
		if len(args) < 4 {
			break
		}
		project := args[2]
		text := strings.TrimSpace(strings.SplitN(action.Text, " ", 4)[3])
		var pErr error
		pErr = f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { _, e := f.authorizedProject(tx, a, project); return e })
		if pErr != nil {
			return pageUnavailable(), nil
		}
		draft, e := f.journal.PrepareProjectShare(ctx, a.Owner, a.Epoch, "human:"+operationID, privatechat.ProjectShareDraft{ProjectID: project, Text: text})
		if e != nil {
			return pageUnavailable(), nil
		}
		return f.renderDraft(ctx, a, draft.ID)
	case "draft":
		if len(args) != 3 {
			break
		}
		return f.renderDraft(ctx, a, args[2])
	}
	return projectPage("项目资料与分享", "请重新打开 /project。"), nil
}
func pageUnavailable() control.FeishuPageView {
	return projectPage("项目资料", "当前身份、项目权限或资料版本无法核对，请稍后重试。")
}
func (f privateProjectFacade) renderDraft(ctx context.Context, a privateProjectAuthority, id string) (control.FeishuPageView, error) {
	d, e := f.journal.ProjectShareDraft(ctx, a.Owner, a.Epoch, id)
	if e != nil {
		return pageUnavailable(), nil
	}
	var name string
	e = f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		p, x := f.authorizedProject(tx, a, d.ProjectID)
		if x == nil {
			name = p.Name
		}
		return x
	})
	if e != nil {
		return pageUnavailable(), nil
	}
	if d.MemoryID != "" {
		m, x := f.journal.ReadMemory(ctx, a.Owner, a.Epoch, d.MemoryID, d.MemoryRevision)
		if x != nil || m.Content != d.Text {
			return projectPage("分享已失效", "私人记忆已变更，请重新准备。"), nil
		}
	}
	var resourceHash string
	if d.ResourceID != "" {
		v, x := f.readVersion(ctx, a, d.ProjectID, d.ResourceID, d.VersionID)
		if x != nil || !v.Saved || v.ContentHash == "" {
			return pageUnavailable(), nil
		}
		resourceHash = v.ContentHash
	}
	hash, e := privatechat.ProjectShareDraftDigest(d)
	if e != nil {
		return pageUnavailable(), nil
	}
	lines := []string{"目标项目：" + name, "仅分享下列内容：", d.Text}
	if d.ResourceID != "" {
		lines = append(lines, "固定资料版本："+resourceHash)
	}
	view := projectPage("确认分享", lines...)
	view.RelatedButtons = []control.CommandCatalogButton{projectButton("确认分享", "confirm "+d.ID+" "+hash), projectButton("返回", "drafts")}
	return view, nil
}
