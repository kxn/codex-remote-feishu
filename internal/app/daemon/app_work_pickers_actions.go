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
	source.BootstrapSourceMessageID, _ = values["bootstrap_source_message_id"].(string)
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
		return a.queuePrivatePanel(ctx, source, "workspace", "已切换到项目相关的私人讨论。")
	}
	if view.Kind == "execution.location-picker" {
		return a.applyTeamLocationChoice(ctx, source, view, action, values)
	}
	return "", workstore.ErrScopeDenied
}

type teamCardElement struct {
	Tag      string            `json:"tag"`
	Name     string            `json:"name"`
	Elements []teamCardElement `json:"elements"`
	Options  []struct {
		Value string `json:"value"`
	} `json:"options"`
	Behaviors []struct {
		Value map[string]any `json:"value"`
	} `json:"behaviors"`
}

func teamFrozenCardElements(tx *workstore.Tx, view workstore.UIView, interaction workstore.HumanInteraction) ([]teamCardElement, error) {
	if err := tx.ValidateViewInteraction(view, interaction); err != nil {
		return nil, err
	}
	body, err := tx.GetContent(view.ScopeID, view.BodyRef)
	if err != nil {
		return nil, err
	}
	var projection struct {
		Card struct {
			Body struct {
				Elements []teamCardElement `json:"elements"`
			} `json:"body"`
		} `json:"card"`
	}
	err = json.Unmarshal(body.Data, &projection)
	return projection.Card.Body.Elements, err
}

func teamFrozenCallback(elements []teamCardElement, values map[string]any, action string) map[string]any {
	for _, element := range elements {
		for _, behavior := range element.Behaviors {
			if behavior.Value["action"] == action && reflect.DeepEqual(values, behavior.Value) {
				return behavior.Value
			}
		}
		if child := teamFrozenCallback(element.Elements, values, action); child != nil {
			return child
		}
	}
	return nil
}

func (a *App) teamFrozenPickerValues(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView, action string) (map[string]any, error) {
	var values map[string]any
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		elements, e := teamFrozenCardElements(tx, view, interaction)
		if e != nil {
			return e
		}
		values = teamFrozenCallback(elements, interaction.Envelope.Values, action)
		if values == nil {
			return fmt.Errorf("这项操作不属于当前卡片，请重新打开菜单")
		}
		return nil
	})
	return values, err
}
