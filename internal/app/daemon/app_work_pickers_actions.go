package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func (a *App) handleTeamPickerAction(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) (string, error) {
	if view.Kind != "private.project-picker" && view.Kind != "execution.location-picker" {
		return "", workstore.ErrScopeDenied
	}
	values, err := a.teamFrozenPickerValues(ctx, interaction, view, action)
	if err != nil {
		return "", err
	}
	revision := view.TargetRevision
	if view.Kind == "execution.location-picker" {
		frozen, _ := values["navigation_revision"].(string)
		if frozen != "" {
			revision, err = strconv.ParseInt(frozen, 10, 64)
			if err != nil {
				return "", err
			}
		}
	}
	source := teamHumanSource{NavigationRevision: revision, Key: interaction.ID, ScopeID: view.ScopeID, PrincipalID: interaction.PrincipalID, RoleID: interaction.Gateway.RoleID, Tenant: interaction.Envelope.Tenant, ChatID: interaction.Envelope.ChatID, MessageID: interaction.Envelope.MessageID, Gateway: interaction.Gateway, SenderIDs: interaction.Envelope.SenderIDs, Authority: workstore.PrivateActionSource{PersonalScopeID: view.ScopeID, InteractionID: interaction.ID}}
	if strings.HasSuffix(action, ".page") {
		text, _ := values["page"].(string)
		page, e := strconv.Atoi(text)
		if e != nil {
			return "", e
		}
		if view.Kind == "private.project-picker" {
			return a.queueTeamProjectPickerPage(ctx, source, page)
		}
		return a.queueTeamResourcePickerPage(ctx, source, page)
	}
	if action == "picker.project.select" && view.Kind == "private.project-picker" {
		scope, _ := values["project_scope_id"].(string)
		if err = a.navigateFromSource(ctx, source, "enter_project", scope, "", view.TargetRevision); err != nil {
			return "", err
		}
		return a.queuePrivatePanel(ctx, source, "menu", "已进入项目视图，私聊原文仍只留在这里。")
	}
	if view.Kind == "execution.location-picker" {
		return a.applyTeamLocationChoice(ctx, source, view, action, values)
	}
	return "", workstore.ErrScopeDenied
}

func (a *App) teamFrozenPickerValues(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) (map[string]any, error) {
	var values map[string]any
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.ValidateViewInteraction(view, interaction); e != nil {
			return e
		}
		body, e := tx.GetContent(view.ScopeID, view.BodyRef)
		if e != nil {
			return e
		}
		var projection struct {
			Card json.RawMessage `json:"card"`
		}
		if e = json.Unmarshal(body.Data, &projection); e != nil {
			return e
		}
		var card struct {
			Body struct {
				Elements []struct {
					Behaviors []struct {
						Value map[string]any `json:"value"`
					} `json:"behaviors"`
				} `json:"elements"`
			} `json:"body"`
		}
		if e = json.Unmarshal(projection.Card, &card); e != nil {
			return e
		}
		for _, element := range card.Body.Elements {
			for _, behavior := range element.Behaviors {
				if behavior.Value["action"] != action {
					continue
				}
				matches := true
				for key, value := range behavior.Value {
					if !reflect.DeepEqual(interaction.Envelope.Values[key], value) {
						matches = false
						break
					}
				}
				if matches {
					values = behavior.Value
					return nil
				}
			}
		}
		return fmt.Errorf("该选择不属于这张卡片，请重新打开选择器")
	})
	if err != nil {
		return nil, err
	}

	return values, nil
}
