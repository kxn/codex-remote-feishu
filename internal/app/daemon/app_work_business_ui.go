package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type artifactCursor struct {
	References  []work.DeliveryRef
	Index, Page int
}

func (a *App) handleTeamBusinessView(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	projectScope := false
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, err := tx.GetScope(view.ScopeID)
		if err != nil {
			return err
		}
		projectScope = scope.Kind == work.ScopeProject
		return nil
	}); err != nil {
		return err
	}
	if projectScope {
		if err := a.verifyWorkMembership(ctx, interaction.PrincipalID, view.ScopeID, interaction.Envelope.Tenant, interaction.Envelope.SenderIDs, interaction.Gateway); err != nil {
			return err
		}
	}
	if action == "artifacts.download" {
		return a.sendTeamSavedArtifactAsset(ctx, interaction, view)
	}
	if action == "decision.choose" {
		choice, _ := interaction.Envelope.Values["choice"].(string)
		result, err := a.teamRuntime.service.RespondHumanDecision(ctx, interaction.ID, view.ID, choice)
		if err != nil {
			return err
		}
		return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, result.OperationID)
		})
	}
	if action != "artifacts.open" && action != "artifacts.page" {
		return fmt.Errorf("unknown business view action")
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		if projectScope {
			if err := tx.RequireProjectPrincipalAccess(view.ScopeID, interaction.PrincipalID, time.Now().UTC()); err != nil {
				return err
			}
		}
		cursor := artifactCursor{}
		if body, err := tx.GetContent(view.ScopeID, view.ID+":artifact-cursor"); err == nil {
			if err = json.Unmarshal(body.Data, &cursor); err != nil {
				return err
			}
		} else if err == workstore.ErrNotFound {
			switch view.Kind {
			case "business.decision":
				request, err := tx.GetDecisionRequest(view.ScopeID, view.TargetID)
				if err != nil {
					return err
				}
				if request.Presentation == nil {
					return fmt.Errorf("decision has no saved material")
				}
				cursor.References = request.Presentation.References
			case "project.artifacts":
				delivery, err := tx.GetDelivery(view.ScopeID, view.TargetID)
				if err != nil {
					return err
				}
				cursor.References = delivery.References
			default:
				return workstore.ErrScopeDenied
			}
		} else {
			return err
		}
		if len(cursor.References) == 0 {
			return fmt.Errorf("no saved artifact was attached to this object")
		}
		if action == "artifacts.page" {
			index, err := businessPageInt(interaction.Envelope.Values["reference"])
			if err != nil {
				return err
			}
			page, err := businessPageInt(interaction.Envelope.Values["page"])
			if err != nil {
				return err
			}
			if index < 0 || index >= len(cursor.References) || page < 0 {
				return fmt.Errorf("artifact page out of range")
			}
			cursor.Index = index
			cursor.Page = page
		}
		ref := cursor.References[cursor.Index]
		version, err := tx.GetResourceVersion(view.ScopeID, ref.VersionID)
		if err != nil {
			return err
		}
		resource, err := tx.GetResource(view.ScopeID, ref.ResourceID)
		if err != nil {
			return err
		}
		if version.ResourceID != resource.ID || !version.Saved || version.ContentRef == "" || ref.Current {
			return fmt.Errorf("artifact is not an independently saved version")
		}
		content, err := tx.GetContent(view.ScopeID, version.ContentRef)
		if err != nil {
			return err
		}
		if content.SHA256 != strings.TrimPrefix(version.ContentHash, "sha256:") {
			return fmt.Errorf("saved artifact integrity mismatch")
		}
		rendered, err := renderSavedArtifact(content)
		if err != nil {
			return err
		}
		runes := []rune(rendered)
		const pageSize = 3500
		if cursor.Page > len(runes)/pageSize {
			return fmt.Errorf("artifact page out of range")
		}
		offset := cursor.Page * pageSize
		if offset < 0 || offset > len(runes) {
			return fmt.Errorf("artifact page out of range")
		}
		end := offset + pageSize
		if end > len(runes) {
			end = len(runes)
		}
		id := workui.ID(interaction.ID, "artifact", strconv.Itoa(cursor.Index), strconv.Itoa(cursor.Page))
		panel := workui.Panel{Title: resource.Purpose + " · 已保存版本", Paragraphs: []string{fmt.Sprintf("资料%d/%d · 正文第%d页；以下是发布/请求决定时保存的内容。", cursor.Index+1, len(cursor.References), cursor.Page+1), string(runes[offset:end]), "当前源文档（可能已经更新）：" + resource.CanonicalURI}}
		if !version.Complete {
			panel.Paragraphs = append(panel.Paragraphs, "此份保存内容不完整，请勿将它视为全文。")
		} else if version.Consistency == work.ConsistencyObserved {
			panel.Paragraphs = append(panel.Paragraphs, "这是读取时保存的数据；多次读取不保证来自同一个平台修订。")
		}
		button := func(label string, index, page int) {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: label, Action: "artifacts.page", Values: map[string]any{"reference": index, "page": page}})
		}
		if cursor.Page > 0 {
			button("上一页", cursor.Index, cursor.Page-1)
		}
		if end < len(runes) {
			button("下一页", cursor.Index, cursor.Page+1)
		}
		if cursor.Index > 0 {
			button("上一份资料", cursor.Index-1, 0)
		}
		if cursor.Index+1 < len(cursor.References) {
			button("下一份资料", cursor.Index+1, 0)
		}
		var saved struct {
			Assets []workservice.SavedDocumentAsset `json:"assets"`
		}
		if json.Unmarshal(content.Data, &saved) == nil {
			for index, asset := range saved.Assets {
				label := fmt.Sprintf("下载附件%d", index+1)
				if strings.HasPrefix(asset.MediaType, "image/") {
					label = fmt.Sprintf("查看图片%d", index+1)
				}
				panel.Buttons = append(panel.Buttons, workui.Button{Label: label, Action: "artifacts.download", Values: map[string]any{"asset": index}})
			}
		}
		card, err := workui.Card(id, panel)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(cursor)
		if err = tx.PutContent(work.NewContent(id+":artifact-cursor", view.ScopeID, "application/json", body, time.Now().UTC())); err != nil {
			return err
		}
		next := view
		next.ExternalMessageID = ""
		next.ReplacesViewID = ""
		next.BodyRef = ""
		next.OperationID = ""
		next.Invalidated = false
		next.Revision = 0
		next.ID = id
		next.Kind = "project.artifacts"
		next.SurfaceKey = "artifact-reader:" + view.TargetID
		next.ReplyToMessageID = ""
		next.PrincipalID = interaction.PrincipalID
		if err = workui.ReplaceFromInteraction(tx, &next, interaction.ID); err != nil {
			return err
		}
		next, err = workui.Queue(tx, next, card)
		if err != nil {
			return err
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, next.ID)
	})
}
func businessPageInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		if v != float64(int(v)) {
			return 0, fmt.Errorf("invalid page index")
		}
		return int(v), nil
	case json.Number:
		n, err := strconv.Atoi(string(v))
		return n, err
	case string:
		n, err := strconv.Atoi(v)
		return n, err
	default:
		return 0, fmt.Errorf("missing page index")
	}
}

// queueTeamBusinessDecisions publishes one append-only human decision card for
// each exact request revision. Role-owned choices remain agent tool work.
func (a *App) queueTeamBusinessDecisions(tx *workstore.Tx, project work.Project) error {
	requests, err := tx.ListDecisionRequests(project.ScopeID)
	if err != nil {
		return err
	}
	for _, request := range requests {
		if request.State != work.DecisionOpen || request.Kind != work.DecisionBusiness || request.Owner.Kind != work.ActorPrincipal || request.Presentation == nil {
			continue
		}
		id := workui.ID("business.decision", request.ID, strconv.FormatInt(request.Revision, 10))
		if _, err = tx.GetUIView(id); err == nil {
			continue
		} else if err != workstore.ErrNotFound {
			return err
		}
		body, err := tx.GetContent(project.ScopeID, request.Presentation.PromptRef)
		if err != nil {
			return err
		}
		destination, err := workui.Destination(tx, project.ScopeID, project.LeaderRoleID, request.AttemptID)
		if err != nil {
			return err
		}
		panel := workui.Panel{Title: request.Presentation.Title, Paragraphs: []string{string(body.Data), "请针对本卡展示的要求和保存版本作决定；也可以直接告诉负责人您的意见。"}}
		for _, choice := range request.Presentation.Choices {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: choice.Label, Action: "decision.choose", Values: map[string]any{"choice": choice.Value}})
		}
		if len(request.Presentation.References) > 0 {
			panel.Buttons = append(panel.Buttons, workui.Button{Label: "先看已保存资料", Action: "artifacts.open"})
		}
		card, err := workui.Card(id, panel)
		if err != nil {
			return err
		}
		if len(card) > 28*1024 {
			return fmt.Errorf("decision question requires a concise summary and saved supporting material")
		}
		destination.ID = id
		destination.SurfaceKey = "business.decision:" + request.ID
		destination.Kind = "business.decision"
		destination.TargetID = request.ID
		destination.TargetRevision = request.Revision
		destination.PrincipalID = request.Owner.ID
		if _, err = workui.Queue(tx, destination, card); err != nil {
			return err
		}
	}
	return nil
}
