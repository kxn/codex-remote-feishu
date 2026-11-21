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
	"strconv"
	"time"
)

func (a *App) handleTeamProjectDirectory(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	if err := a.verifyWorkMembership(ctx, interaction.PrincipalID, view.ScopeID, interaction.Envelope.Tenant, interaction.Envelope.SenderIDs, interaction.Gateway); err != nil {
		return err
	}
	if action == "project.directory.open" && view.Kind == "project.status" {
		return a.openTeamProjectDirectory(ctx, interaction, view)
	}
	if action != "project.directory.select" || view.Kind != "project.directory" {
		return workstore.ErrScopeDenied
	}
	result, err := a.teamRuntime.service.SelectProjectDirectoryFromInteraction(ctx, interaction.ID, view.ID)
	if err != nil {
		return err
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		id := workui.ID("project.directory.result", interaction.ID)
		card, err := workui.Card(id, workui.Panel{Title: "新资料目录已保存", Paragraphs: []string{"会核对或移动同一份项目首页，固定资料不会按标题另建副本。若之前的创建或移动尚未确认，会先保留新选择并等待核查。"}})
		if err != nil {
			return err
		}
		notice := view
		notice.ID = id
		notice.SurfaceKey = id
		notice.Kind = "project.directory.result"
		notice.TargetID = result.ObjectID
		notice.TargetRevision = result.Revision
		notice.PrincipalID = interaction.PrincipalID
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
func (a *App) openTeamProjectDirectory(ctx context.Context, interaction workstore.HumanInteraction, previous workstore.UIView) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(previous, interaction); err != nil {
			return err
		}
		if err := tx.RequireProjectPrincipalAccess(previous.ScopeID, interaction.PrincipalID, time.Now()); err != nil {
			return err
		}
		scope, err := tx.GetScope(previous.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopeProject {
			return workstore.ErrScopeDenied
		}
		project, err := tx.GetProject(scope.ProjectID)
		if err != nil {
			return err
		}
		home, err := tx.GetProjectHome(project.ID)
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(home.OperationID)
		if err != nil {
			return err
		}
		if op.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}) || op.State == work.OperationConfirmed || project.State == work.ProjectArchiving || project.State == work.ProjectArchived {
			return fmt.Errorf("仅建立项目的用户可在建立完成前修改资料目录")
		}
		id := workui.ID("project.directory", interaction.ID, strconv.FormatInt(home.DirectoryGeneration, 10))
		target := workservice.ProjectDirectoryInteractionTarget{ProjectID: project.ID, ExpectedProjectRevision: project.Revision, ExpectedDirectoryGeneration: home.DirectoryGeneration}
		raw, _ := json.Marshal(target)
		if err = tx.PutContent(work.NewContent(id+":directory_target", scope.ID, "application/json", raw, time.Now())); err != nil {
			return err
		}
		card, err := projectDirectoryCard(id, project.Name)
		if err != nil {
			return err
		}
		view := workstore.UIView{ID: id, ScopeID: scope.ID, SurfaceKey: "project.directory:" + project.ID, Kind: "project.directory", TargetID: project.ID, TargetRevision: project.Revision, Binding: previous.Binding, Tenant: previous.Tenant, ChatID: previous.ChatID, ChannelGeneration: previous.ChannelGeneration, PrincipalID: interaction.PrincipalID}
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
func projectDirectoryCard(id, name string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"title": cardkit.PlainText("选择“" + name + "”的资料目录")}, "body": map[string]any{"elements": []any{cardkit.PlainTextBlockElement("粘贴已有 Drive 文件夹、Wiki 父节点或已有空间根的链接。只移动同一份项目首页；不会新建 Wiki 空间，也不会覆盖人工置顶。旧请求未确认时会先核查，可能需要您提供实际文档链接。"), map[string]any{"tag": "form", "name": "project_directory", "elements": []any{map[string]any{"tag": "input", "name": "document_container", "required": true, "placeholder": cardkit.PlainText("粘贴实际资料目录链接")}, map[string]any{"tag": "button", "name": "select_directory", "action_type": "form_submit", "text": cardkit.PlainText("保存新目录并核对迁移"), "behaviors": []any{map[string]any{"type": "callback", "value": map[string]any{"team_view": id, "action": "project.directory.select"}}}}}}}}})
}
