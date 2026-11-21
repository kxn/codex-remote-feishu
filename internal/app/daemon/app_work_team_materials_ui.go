package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strconv"
	"time"
)

func (a *App) handleTeamMaterials(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) error {
	if err := a.verifyWorkMembership(ctx, interaction.PrincipalID, view.ScopeID, interaction.Envelope.Tenant, interaction.Envelope.SenderIDs, interaction.Gateway); err != nil {
		return err
	}
	if action == "team.materials.share" && view.Kind == "team.materials" {
		if err := a.teamRuntime.service.ShareTeamMaterialsFromInteraction(ctx, interaction.ID, view.ID); err != nil {
			return err
		}
		return a.teamMaterialsSelectionNotice(ctx, interaction, view, "已记录共享范围，正在核对本项目群的访问。")
	}
	if action == "team.materials.select" && view.Kind == "team.materials" {
		_, err := a.teamRuntime.service.SelectTeamMaterialsFromInteraction(ctx, interaction.ID, view.ID)
		if err != nil {
			return err
		}
		return a.teamMaterialsSelectionNotice(ctx, interaction, view, "已保存团队资料选择，正在连接；结果将显示在项目状态和团队资料入口。")
	}
	if action != "team.materials.open" {
		return workstore.ErrScopeDenied
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		if err := tx.RequireProjectPrincipalAccess(view.ScopeID, interaction.PrincipalID, time.Now()); err != nil {
			return err
		}
		scope, err := tx.GetScope(view.ScopeID)
		if err != nil {
			return err
		}
		team, err := tx.GetTeam(scope.TeamID)
		if err != nil {
			return err
		}
		owner, err := tx.GetTeamOwnership(team.ID)
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		id := workui.ID("team.materials", interaction.ID, strconv.FormatInt(team.Revision, 10))
		canCreate, err := tx.TeamMaterialsCanSelectNew(team)
		if err != nil {
			return err
		}
		channel, err := tx.GetProjectChannelBinding(view.Tenant, view.ChatID)
		if err != nil {
			return err
		}
		allowed, err := tx.TeamMaterialsProjectAllowed(team, channel)
		if err != nil {
			return err
		}
		if !allowed && team.Materials != nil {
			home := *team.Materials
			home.URL = ""
			home.LastError = "本项目群尚未获团队所有者共享此资料。"
			team.Materials = &home
		}
		body, err := teamMaterialsCard(id, team, owner.PrincipalID == interaction.PrincipalID, canCreate, !allowed)
		if err != nil {
			return err
		}
		next := workstore.UIView{ID: id, ScopeID: scope.ID, SurfaceKey: "team.materials:" + team.ID + ":" + interaction.PrincipalID, Kind: "team.materials", TargetID: team.ID, TargetRevision: team.Revision, Binding: view.Binding, Tenant: view.Tenant, ChatID: view.ChatID, ChannelGeneration: view.ChannelGeneration, PrincipalID: interaction.PrincipalID}
		if _, err = workui.Queue(tx, next, body); err != nil {
			return err
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, id)
	})
}
func teamMaterialsCard(id string, team work.Team, owner, canCreate, shareNeeded bool) (json.RawMessage, error) {
	text := "团队：" + team.Name + "\n同一团队的项目共用此原件；明确共享的项目群成员可共同编辑，归档项目不会删除团队资料。"
	mode := "managed"
	if team.Materials == nil {
		text += "\n团队资料待连接。"
	} else {
		names := map[string]string{"pending": "正在连接", "ready": "已连接", "unavailable": "待连接", "unknown": "建立结果待核查"}
		text += "\n状态：" + names[team.Materials.State]
		if team.Materials.URL != "" {
			text += "\n团队首页：" + team.Materials.URL
		}
		if team.Materials.LastError != "" {
			text += "\n原因：" + team.Materials.LastError
		}
		mode = "existing"
	}
	if !owner {
		return workui.Card(id, workui.Panel{Title: "团队共享资料", Paragraphs: []string{text, "需要调整资料入口时，请团队所有者在此操作。"}})
	}
	text += "\n在此选择资料会将该原件共享给当前项目群（可编辑），其他项目需另行共享。\n选择已有 Drive 文件夹或 Wiki 父节点，会在所选位置创建团队首页；选择现有首页则原位使用，不改正文。已开始建立后只能选择现有首页，避免重复创建。"
	if canCreate {
		mode = "managed"
	}
	options := []any{}
	add := func(value, label string) {
		options = append(options, map[string]any{"text": cardkit.PlainText(label), "value": value})
	}
	if canCreate {
		add("managed", "系统管理的 Drive 资料根")
		add("drive", "已有 Drive 文件夹")
		add("wiki", "已有 Wiki 父节点／空间")
	}
	add("existing", "已有团队首页（Docx／Wiki 文档）")
	elements := []any{cardkit.PlainTextBlockElement(text)}
	if team.Materials != nil {
		elements = append(elements, map[string]any{"tag": "button", "text": cardkit.PlainText("连接当前项目群访问（可编辑）"), "behaviors": []any{map[string]any{"type": "callback", "value": map[string]any{"team_view": id, "action": "team.materials.share"}}}})
	}
	elements = append(elements, map[string]any{"tag": "form", "name": "team_materials", "elements": []any{
		map[string]any{"tag": "select_static", "name": "materials_mode", "required": true, "initial_option": mode, "options": options},
		map[string]any{"tag": "input", "name": "materials_url", "placeholder": cardkit.PlainText("粘贴实际资料入口链接；系统托管无需填写")},
		map[string]any{"tag": "button", "name": "connect_materials", "action_type": "form_submit", "text": cardkit.PlainText("保存选择并连接"), "behaviors": []any{map[string]any{"type": "callback", "value": map[string]any{"team_view": id, "action": "team.materials.select"}}}},
	}})
	return json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"title": cardkit.PlainText("团队共享资料")}, "body": map[string]any{"elements": elements}})
}
func (a *App) teamMaterialsSelectionNotice(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, text string) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		id := workui.ID("team.materials.result", interaction.ID)
		card, err := workui.Card(id, workui.Panel{Title: "团队资料", Paragraphs: []string{text}})
		if err != nil {
			return err
		}
		notice := workstore.UIView{ID: id, ScopeID: view.ScopeID, SurfaceKey: id, Kind: "team.materials.result", TargetID: view.TargetID, TargetRevision: 1, Binding: view.Binding, Tenant: view.Tenant, ChatID: view.ChatID, ChannelGeneration: view.ChannelGeneration, PrincipalID: interaction.PrincipalID}
		_, err = workui.Queue(tx, notice, card)
		return err
	})
}
