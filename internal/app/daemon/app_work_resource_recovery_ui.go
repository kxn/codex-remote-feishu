package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func resourceRecoveryKind(kind string) string {
	switch kind {
	case "resource.document.create":
		return "新建资料文档"
	case "resource.document.append":
		return "补充文档内容"
	case "resource.document.update":
		return "修改文档段落"
	case "resource.sheet.write":
		return "修改表格区域"
	}
	return ""
}

// These cards ask for actual human choices. No periodic lane retries a write or
// automatically adopts a source merely because it has a matching title.
func (a *App) runTeamResourceWriteRecoveryViews(ctx context.Context) error {
	if a.teamRuntime == nil {
		return nil
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		queued := 0
		for _, scope := range scopes {
			if scope.Kind != work.ScopeProject && scope.Kind != work.ScopePersonal {
				continue
			}
			ops, err := tx.ListOperations(scope.ID)
			if err != nil {
				return err
			}
			for _, op := range ops {
				if resourceRecoveryKind(op.Kind) == "" || (op.State != work.OperationUnknown && op.State != work.OperationSending) {
					continue
				}
				if a.teamRuntime.service.ResourceWriteCallInFlight(op.ID) {
					continue
				}
				owner, err := tx.ResourceRecoveryOwner(scope.ID)
				if err != nil {
					return err
				}
				binding, tenant, chat, generation, err := resourceRecoveryDestination(tx, scope, op)
				if err != nil {
					return err
				}
				sealed := false
				for _, step := range op.Steps {
					sealed = sealed || step.ID == "human_resource_recovery"
				}
				if sealed {
					valid, err := tx.ResourceWriteWasRecovered(op)
					if err != nil {
						return err
					}
					if !valid {
						return workstore.ErrScopeDenied
					}
					for _, step := range op.Steps {
						if !strings.HasPrefix(step.ID, "late_original_receipt:") {
							continue
						}
						id := workui.ID("resource.write.recovery.late", op.ID, step.ID)
						if _, e := tx.GetUIView(id); e == nil {
							continue
						} else if e != workstore.ErrNotFound {
							return e
						}
						card, err := workui.Card(id, workui.Panel{Title: "之前的资料请求出现迟到结果", Paragraphs: []string{"原请求后来返回了实际结果，可能已改变原来的文档或表格。此前您采用的观察和决定仍保留，没有被自动替换，也没有再次重发原请求。", "请让负责人重新核对当前资料；迟到回执已与原请求一同保存。"}})
						if err != nil {
							return err
						}
						view := workstore.UIView{ID: id, ScopeID: scope.ID, SurfaceKey: id, Kind: "resource.write.recovery.late", TargetID: op.ID, TargetRevision: op.Revision, Binding: binding, Tenant: tenant, ChatID: chat, ChannelGeneration: generation, PrincipalID: owner}
						if _, err = workui.Queue(tx, view, card); err != nil {
							return err
						}
						queued++
					}
					continue
				}
				id := workui.ID("resource.write.recovery", op.ID, strconv.FormatInt(op.Revision, 10))
				if _, err = tx.GetUIView(id); err == nil {
					continue
				} else if err != workstore.ErrNotFound {
					return err
				}
				body, err := tx.GetContent(scope.ID, op.RequestRef)
				if err != nil {
					return err
				}
				var request struct {
					Resource work.Resource                              `json:"resource"`
					Create   *workservice.CreateResourceDocumentRequest `json:"create"`
					Sheet    *workservice.WriteResourceSheetRequest     `json:"sheet"`
				}
				if err = json.Unmarshal(body.Data, &request); err != nil {
					return err
				}
				description := resourceRecoveryKind(op.Kind)
				if request.Create != nil {
					description += "：“" + request.Create.Title + "”"
				} else {
					description += "\n原资料链接：" + resourceRecoveryURL(request.Resource.CanonicalURI)
				}
				if request.Sheet != nil {
					description += "\n指定区域：" + request.Sheet.Range
				}
				card, err := resourceRecoveryInspectCard(id, description, request.Create != nil)
				if err != nil {
					return err
				}
				view := workstore.UIView{ID: id, ScopeID: scope.ID, ContextID: op.ContextID, SurfaceKey: "resource.write.recovery:" + op.ID, Kind: "resource.write.recovery", TargetID: op.ID, TargetRevision: op.Revision, Binding: binding, Tenant: tenant, ChatID: chat, ChannelGeneration: generation, PrincipalID: owner}
				if _, err = workui.Queue(tx, view, card); err != nil {
					return err
				}
				queued++
				if queued >= 10 {
					return nil
				}
			}
		}
		return nil
	})
}
func resourceRecoveryDestination(tx *workstore.Tx, scope work.WorkScope, op work.Operation) (work.RoleBotBinding, string, string, int64, error) {
	role := scope.RoleID
	tenant, chat := "", ""
	generation := int64(0)
	if scope.Kind == work.ScopeProject {
		project, err := tx.GetProject(scope.ProjectID)
		if err != nil {
			return work.RoleBotBinding{}, "", "", 0, err
		}
		role = project.LeaderRoleID
		channels, err := tx.ListProjectChannelBindings(project.ID)
		if err != nil {
			return work.RoleBotBinding{}, "", "", 0, err
		}
		for _, channel := range channels {
			if channel.Active && channel.Purpose == "main" {
				if chat != "" {
					return work.RoleBotBinding{}, "", "", 0, fmt.Errorf("ambiguous project recovery destination")
				}
				tenant, chat, generation = channel.Tenant, channel.ChatID, channel.BindingGeneration
			}
		}
	} else {
		destination, err := tx.GetPersonalContextDestination(scope.ID, op.ContextID, role)
		if err != nil {
			return work.RoleBotBinding{}, "", "", 0, err
		}
		tenant, chat = destination.Tenant, destination.ChatID
	}
	binding, err := tx.GetRoleBotBinding(role)
	if err == nil && chat == "" {
		err = fmt.Errorf("no actual recovery conversation")
	}
	return binding, tenant, chat, generation, err
}
func resourceRecoveryInspectCard(id, description string, create bool) (json.RawMessage, error) {
	elements := []any{cardkit.PlainTextBlockElement(description), cardkit.PlainTextBlockElement("平台没有给出确定回执。这里不会重发原请求，也不会把同名资料当作创建成功。原请求仍可能在远端迟到生效；先核对当前实际资料，再由您决定是否采用该状态。")}
	fields := []any{}
	label := "读取当前目标并核对"
	if create {
		elements = append(elements, cardkit.PlainTextBlockElement("若您决定改用已有文档，请明确选择它的实际链接。不会据此声称原来的创建成功。"))
		fields = append(fields, map[string]any{"tag": "input", "name": "source_url", "required": true, "placeholder": cardkit.PlainText("粘贴您选择改用的已有 Docx／Wiki 文档链接")})
		label = "读取这份已有文档"
	}
	fields = append(fields, map[string]any{"tag": "button", "name": "inspect", "action_type": "form_submit", "text": cardkit.PlainText(label), "behaviors": []any{map[string]any{"type": "callback", "value": map[string]any{"team_view": id, "action": "resource.write.inspect"}}}})
	elements = append(elements, map[string]any{"tag": "form", "name": "resource_recovery", "elements": fields})
	return json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"title": cardkit.PlainText("资料请求需要您核对")}, "body": map[string]any{"elements": elements}})
}
func (a *App) handleTeamResourceWriteRecovery(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	var project bool
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, e := tx.GetScope(view.ScopeID)
		project = scope.Kind == work.ScopeProject
		return e
	}); err != nil {
		return err
	}
	if project {
		if err := a.verifyWorkMembership(ctx, interaction.PrincipalID, view.ScopeID, interaction.Envelope.Tenant, interaction.Envelope.SenderIDs, interaction.Gateway); err != nil {
			return err
		}
	}
	if action == "resource.write.inspect" && view.Kind == "resource.write.recovery" {
		inspection, err := a.teamRuntime.service.InspectResourceWriteRecovery(ctx, interaction.ID, view.ID)
		if err != nil {
			return err
		}
		return a.queueResourceRecoveryConfirmation(ctx, interaction, view, inspection)
	}
	if action != "resource.write.adopt" || view.Kind != "resource.write.recovery.confirm" {
		return workstore.ErrScopeDenied
	}
	result, err := a.teamRuntime.service.AdoptResourceWriteRecovery(ctx, interaction.ID, view.ID)
	if err != nil {
		return err
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		id := workui.ID("resource.write.recovery.result", interaction.ID)
		card, err := workui.Card(id, workui.Panel{Title: "已按您的决定采用核对结果", Paragraphs: []string{"原请求仍保留为未知，已封存且不会重发。采用的是刚才显示的已读取对象或状态，不代表原请求成功、被撤销或保证不会迟到生效。若后来返回结果，会保留并提醒负责人再次核对。"}})
		if err != nil {
			return err
		}
		notice := view
		notice.ID = id
		notice.SurfaceKey = id
		notice.Kind = "resource.write.recovery.result"
		notice.TargetID = result.OperationID
		notice.TargetRevision = 1
		notice.ReplacesViewID = ""
		notice.ExternalMessageID = ""
		notice.OperationID = ""
		notice.BodyRef = ""
		notice.Revision = 0
		notice.Invalidated = false
		if _, err = workui.Queue(tx, notice, card); err != nil {
			return err
		}
		fresh, err := tx.GetHumanInteraction(interaction.ID)
		if err != nil {
			return err
		}
		return tx.FinishHumanInteraction(fresh.ID, fresh.Revision, id)
	})
}
func (a *App) queueResourceRecoveryConfirmation(ctx context.Context, interaction workstore.HumanInteraction, previous workstore.UIView, inspection workservice.ResourceRecoveryInspection) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		id := workui.ID("resource.write.recovery.confirm", interaction.ID, inspection.Observation.ReadOperationID)
		target, _ := json.Marshal(workservice.ResourceRecoveryViewTarget{ObservationOperationID: inspection.Observation.ReadOperationID})
		if err := tx.PutContent(work.NewContent(id+":resource_recovery", previous.ScopeID, "application/json", target, time.Now())); err != nil {
			return err
		}
		paragraphs := resourceRecoveryPreview(inspection)
		card, err := workui.Card(id, workui.Panel{Title: "是否采用这次实际核对的状态？", Paragraphs: paragraphs, Buttons: []workui.Button{{Label: "采用已核对状态，不重试原请求", Action: "resource.write.adopt"}}})
		if err != nil {
			return err
		}
		view := previous
		view.ID = id
		view.Kind = "resource.write.recovery.confirm"
		view.SurfaceKey = "resource.write.recovery.confirm:" + previous.TargetID
		view.ReplacesViewID = ""
		view.ExternalMessageID = ""
		view.OperationID = ""
		view.BodyRef = ""
		view.Revision = 0
		view.Invalidated = false
		if _, err = workui.Queue(tx, view, card); err != nil {
			return err
		}
		fresh, err := tx.GetHumanInteraction(interaction.ID)
		if err != nil {
			return err
		}
		return tx.FinishHumanInteraction(fresh.ID, fresh.Revision, id)
	})
}
func resourceRecoveryPreview(inspection workservice.ResourceRecoveryInspection) []string {
	p := []string{"实际资料链接：" + resourceRecoveryURL(inspection.Observation.Resource.CanonicalURI), "核对时间：" + inspection.Observation.ObservedAt.Local().Format("01月02日15:04"), "以下是保存的观察摘要；源内容可能继续变化，平台没有强制版本锁。不是原写入成功的回执。"}
	if inspection.Data.Resolved != nil {
		p = append(p, "已按您提供的链接实际读取该文档的身份与权限。确认后仅登记改用这份文档，不接管同名文件。")
	}
	shown := 0
	total := 0
	for _, document := range inspection.Data.Documents {
		total += len(document.Blocks)
	}
	for _, document := range inspection.Data.Documents {
		for _, block := range document.Blocks {
			if shown >= 8 {
				break
			}
			shown++
			p = append(p, fmt.Sprintf("文档观察版本：%d。以下为指定段落摘要。", document.RequestedRevision))
			for i, run := range block.Runs {
				if i >= 2 {
					p = append(p, "该段落还有内容，请从原资料链接核对完整内容。")
					break
				}
				if run.Kind == "text" {
					p = append(p, run.Summary)
				}
			}
		}
	}
	if total > shown {
		p = append(p, fmt.Sprintf("本次实际核对%d个指定段落，卡片仅展示前%d个摘要；完整观察已保存，请同时从源文档核对受影响内容。", total, shown))
	}

	if sheet := inspection.Data.Sheet; sheet != nil {
		p = append(p, "实际表格区域："+sheet.Values.Range)
		for i, row := range sheet.Values.Values {
			if i >= 6 {
				p = append(p, "更多单元格已保存在观察记录中，卡片仅展示前6行，请从源表核对完整区域。")
				break
			}
			cells := []string{}
			for j, cell := range row {
				if j >= 5 {
					cells = append(cells, "更多列请看源表")
					break
				}
				text := savedCell(cell)
				runes := []rune(text)
				if len(runes) > 80 {
					text = string(runes[:80]) + "（更多内容请看源表）"
				}
				cells = append(cells, text)
			}
			p = append(p, strings.Join(cells, " | "))
		}
	}
	p = append(p, "确认表示采用这次实际读取到的对象或相关状态，封存原未知请求并继续。它不撤销原请求，也不能保证原请求不再迟到生效；迟到结果会另行保存并提示重新核对。它不代表您已接受任务成果，也不会自动标记任务完成。")
	return p
}

func resourceRecoveryURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return "原资料链接格式待核对"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	text := parsed.String()
	if len([]rune(text)) > 500 {
		return "资料链接较长，请从既有资料入口核对原链接（完整链接已保存）"
	}
	return text
}
