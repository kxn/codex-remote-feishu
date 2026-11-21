package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workprovision"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type provisionRecoveryView struct {
	Target      workprovision.RecoveryInteractionTarget
	Home        workstore.ProjectHome
	PrincipalID string
}

// runTeamProvisionRecoveryViews is its own lane: candidate listing is external
// read I/O and never runs inside the SQL transaction that queues a card.
func (a *App) runTeamProvisionRecoveryViews(ctx context.Context) error {
	r := a.teamRuntime
	if r == nil || r.provision == nil {
		return nil
	}
	var candidates []provisionRecoveryView
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if scope.Kind != work.ScopeProject {
				continue
			}
			home, err := tx.GetProjectHome(scope.ProjectID)
			if err == workstore.ErrNotFound {
				continue
			}
			if err != nil {
				return err
			}
			op, err := tx.GetOperation(home.OperationID)
			if err != nil {
				return err
			}
			if op.Actor.Kind != work.ActorPrincipal {
				continue
			}
			steps, err := tx.ListProvisionSteps(op.ID)
			if err != nil {
				return err
			}
			for _, step := range steps {
				if step.State != work.OperationUnknown {
					continue
				}
				stepKind := step.StepID
				if workprovision.IsNavigationTabRestoreStep(stepKind) {
					stepKind = "navigation.tab"
				}
				switch stepKind {
				case "materials.document", "materials.folder", "materials.index", "navigation.tab", "navigation.message":
					candidates = append(candidates, provisionRecoveryView{Target: workprovision.RecoveryInteractionTarget{OperationID: op.ID, StepID: step.StepID, ExpectedRevision: step.Revision, HomeDocumentID: home.DocumentID}, Home: home, PrincipalID: op.Actor.ID})
				}
			}
			if home.SyncState == "conflict" {
				candidates = append(candidates, provisionRecoveryView{Target: workprovision.RecoveryInteractionTarget{OperationID: op.ID, StepID: "home.index", ExpectedRevision: home.Revision, HomeDocumentID: home.DocumentID}, Home: home, PrincipalID: op.Actor.ID})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, candidate := range candidates {
		id := workui.ID("project.recovery", candidate.Target.OperationID, candidate.Target.StepID, strconv.FormatInt(candidate.Target.ExpectedRevision, 10))
		exists := false
		if err = r.store.Within(ctx, func(tx *workstore.Tx) error {
			_, e := tx.GetUIView(id)
			exists = e == nil
			if e == workstore.ErrNotFound {
				return nil
			}
			return e
		}); err != nil {
			failures = append(failures, err)
			continue
		}
		if exists {
			continue
		}
		choices, err := r.provision.RecoveryOptions(ctx, candidate.Target)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		card, err := provisionRecoveryCard(id, candidate.Target.StepID, choices, candidate.Home.URL)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		err = r.store.Within(ctx, func(tx *workstore.Tx) error {
			project, err := tx.GetProject(candidate.Home.ID)
			if err != nil {
				return err
			}
			destination, err := workui.Destination(tx, project.ScopeID, project.LeaderRoleID, "")
			if err != nil {
				return err
			}
			body, _ := json.Marshal(candidate.Target)
			if err = tx.PutContent(work.NewContent(id+":recovery-target", project.ScopeID, "application/json", body, time.Now().UTC())); err != nil {
				return err
			}
			destination.ID = id
			destination.Kind = "project.recovery"
			destination.SurfaceKey = "project.recovery:" + candidate.Target.OperationID + ":" + candidate.Target.StepID
			destination.TargetID = candidate.Target.OperationID
			destination.TargetRevision = candidate.Target.ExpectedRevision
			destination.PrincipalID = candidate.PrincipalID
			_, err = workui.Queue(tx, destination, card)
			return err
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func provisionRecoveryCard(id, step string, choices workprovision.RecoveryChoices, homeURL string) (json.RawMessage, error) {
	title := "项目建立需要核对"
	explanation := "平台调用结果没有可靠回执，系统保留了记录，未重新创建。您可明确选择已存在的资料继续；这不会把原调用改称成功，也不会清理可能已建立的其他资源。"
	elements := []any{cardkit.PlainTextBlockElement(explanation)}
	if homeURL != "" {
		elements = append(elements, cardkit.PlainTextBlockElement("现有项目资料："+homeURL))
	}
	fields := []any{}
	input := func(name, label, placeholder string) {
		fields = append(fields, cardkit.PlainTextBlockElement(label), map[string]any{"tag": "input", "name": name, "required": true, "max_length": 1000, "placeholder": cardkit.PlainText(placeholder)})
	}
	selectField := func(name, label string, options []workprovision.RecoveryChoice) {
		items := []any{}
		for _, option := range options {
			items = append(items, map[string]any{"text": cardkit.PlainText(option.Label), "value": option.ID})
		}
		fields = append(fields, cardkit.PlainTextBlockElement(label), map[string]any{"tag": "select_static", "name": name, "required": true, "placeholder": cardkit.PlainText("明确选择一个已存在的对象"), "options": items})
	}
	buttonLabel := "核实后使用已有资料"
	if workprovision.IsNavigationTabRestoreStep(step) {
		step = "navigation.tab"
	}
	switch step {
	case "materials.document":
		title = "核对项目资料文档"
		input("external_id", "如果您找到了已经创建的文档，请粘贴文档或Wiki链接。", "https://…/docx/… 或 https://…/wiki/…")
	case "materials.folder":
		title = "核对项目资料文件夹"
		input("external_id", "指定已有的项目资料文件夹。", "粘贴飞书文件夹链接")
	case "navigation.tab":
		title = "核对群里的资料入口"
		if len(choices.Primary) == 0 {
			return nil, fmt.Errorf("未找到指向实际项目资料的现有群标签，继续保留未知记录")
		}
		selectField("external_id", "这些是实际查询到、指向项目资料的群标签，请选择要使用的一个。", choices.Primary)
	case "materials.index":
		title = "核对首页已有索引区段"
		if len(choices.Primary) == 0 {
			return nil, fmt.Errorf("未查询到可明确选择的首页区段")
		}
		selectField("external_id", "选择已存在的项目索引段落（会核对是否与当时请求一致）。", choices.Primary)
		selectField("secondary_id", "选择已存在的同步状态段落。", choices.Secondary)
	case "navigation.message":
		title = "核对已发送的资料入口消息"
		input("external_id", "请指定确实存在的那条入口消息。", "已有消息的实际标识；不会重新发送")
	case "home.index":
		title = "首页自动索引与您的修改冲突"
		elements = append(elements, cardkit.PlainTextBlockElement("资料正文和您的修改已经保留。下面的操作只会用当前项目事实替换首页的两段自动索引，文档其他内容保留。平台没有跨编辑者的强制版本锁。"))
		buttonLabel = "按当前项目情况更新自动索引"
	default:
		return nil, fmt.Errorf("unsupported recovery step")
	}
	fields = append(fields, map[string]any{"tag": "button", "name": "recover", "action_type": "form_submit", "text": cardkit.PlainText(buttonLabel), "behaviors": []any{map[string]any{"type": "callback", "value": map[string]any{"team_view": id, "action": "recovery.resolve"}}}})
	elements = append(elements, map[string]any{"tag": "form", "name": "provision_recovery", "elements": fields}, cardkit.PlainTextBlockElement("如果暂时无法确认，可先在项目状态卡暂停。系统不会为了继续流程而按同名资料认领或重建。"))
	raw, err := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"title": cardkit.PlainText(title)}, "body": map[string]any{"elements": elements}})
	if err == nil && len(raw) > 28*1024 {
		return nil, fmt.Errorf("recovery choices exceed card limit")
	}
	return raw, err
}
func (a *App) handleTeamProvisionRecovery(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	if view.Kind != "project.recovery" || action != "recovery.resolve" {
		return workstore.ErrScopeDenied
	}
	if err := a.verifyWorkMembership(ctx, interaction.PrincipalID, view.ScopeID, interaction.Envelope.Tenant, interaction.Envelope.SenderIDs, interaction.Gateway); err != nil {
		return err
	}
	var target workprovision.RecoveryInteractionTarget
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		if err := tx.RequireProjectPrincipalAccess(view.ScopeID, interaction.PrincipalID, time.Now()); err != nil {
			return err
		}
		body, err := tx.GetContent(view.ScopeID, view.ID+":recovery-target")
		if err != nil {
			return err
		}
		return json.Unmarshal(body.Data, &target)
	})
	if err != nil {
		return err
	}
	external, _ := interaction.Envelope.Form["external_id"].(string)
	secondary, _ := interaction.Envelope.Form["secondary_id"].(string)
	recovery := workprovision.Recovery{OperationID: target.OperationID, StepID: target.StepID, ExpectedRevision: target.ExpectedRevision, ExternalID: strings.TrimSpace(external), SecondaryID: strings.TrimSpace(secondary), InteractionID: interaction.ID, ViewID: view.ID}
	if target.StepID == "home.index" {
		recovery.ExternalID = target.HomeDocumentID
	} else if strings.HasPrefix(recovery.ExternalID, "https://") {
		recovery.CandidateURL = recovery.ExternalID
		recovery.ExternalID = ""
	}
	report, err := a.teamRuntime.provision.ResolveUnknown(ctx, recovery)
	if err != nil {
		adopted := false
		for _, step := range report.Steps {
			adopted = adopted || step.StepID == target.StepID && step.State == work.OperationConfirmed
		}
		if !adopted {
			return err
		}
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, target.OperationID)
	})
}
