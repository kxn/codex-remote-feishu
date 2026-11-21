package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
)

func (t *Tx) validateReviewContext(context work.WorkContext, initial bool) error {
	if context.Review == nil {
		return ErrScopeDenied
	}
	spec := context.Review
	source, err := t.GetContext(spec.SourceContextID)
	if err != nil {
		return err
	}
	if source.ScopeID != context.ScopeID || source.RoleID != context.RoleID || source.Purpose == work.PurposeControl || source.Purpose == work.PurposeReview {
		return ErrScopeDenied
	}
	scope, err := t.GetScope(context.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind != work.ScopePersonal {
		return fmt.Errorf("private review context keeps its human source private")
	}
	expectedScope := source.ScopeID
	if source.ProjectViewScopeID != "" {
		expectedScope = source.ProjectViewScopeID
	}
	if expectedScope != spec.ResourceScopeID {
		return ErrScopeDenied
	}
	location, err := t.GetLocation(spec.LocationID)
	if err != nil {
		return err
	}
	if initial && location.Generation != spec.LocationGeneration {
		return ErrConflict
	}
	if initial && spec.ResourceScopeID != scope.ID {
		if err = t.RequireProjectPrincipalAccess(spec.ResourceScopeID, scope.PrincipalID, context.CreatedAt); err != nil {
			return err
		}
	}
	if initial {
		action := PrivateActionSource{PersonalScopeID: scope.ID}
		switch spec.Source.Kind {
		case "private_command":
			action.CommandID = spec.Source.ID
			command, e := t.GetPrivateCommand(action.CommandID)
			if e != nil {
				return e
			}
			if command.ContextID != source.ID || (len(strings.Fields(command.Text)) == 0 || strings.Fields(command.Text)[0] != "/review") {
				return ErrScopeDenied
			}
		case "human_interaction":
			action.InteractionID = spec.Source.ID
			interaction, e := t.GetHumanInteraction(action.InteractionID)
			if e != nil {
				return e
			}
			viewID, _ := interaction.Envelope.Values["team_view"].(string)
			view, e := t.GetUIView(viewID)
			if e != nil {
				return e
			}
			actionName, _ := interaction.Envelope.Values["action"].(string)
			if view.ContextID != source.ID || !strings.Contains(actionName, "review") {
				return ErrScopeDenied
			}
		case "sdk_menu":
			action.MenuInteractionID = spec.Source.ID
			menu, e := t.GetMenuInteraction(action.MenuInteractionID)
			if e != nil {
				return e
			}
			if menu.Envelope.EventKey != "review" || menu.ContextID != source.ID {
				return ErrScopeDenied
			}
		default:
			return ErrScopeDenied
		}
		_, verified, e := t.ValidatePrivateAction(action)
		if e != nil {
			return e
		}
		if verified != spec.Source {
			return ErrScopeDenied
		}
		allowed := false
		preference, e := t.GetExecutionPreference(spec.ResourceScopeID)
		if e == nil {
			for _, id := range preference.SelectedLocationIDs {
				if id == location.ID {
					allowed = true
				}
			}
		} else if e != ErrNotFound {
			return e
		}
		if location.IsolatedContextID == source.ID {
			allowed = true
		}
		if !allowed {
			contextID := source.ID
			if source.ProjectContextID != "" {
				contextID = source.ProjectContextID
			}
			desired, e := t.GetDesiredExecutionLocation(contextID)
			if e == nil && desired.LocationID == location.ID && desired.LocationGeneration == location.Generation {
				for _, root := range preference.SelectedLocationIDs {
					if root == desired.RootLocationID {
						allowed = true
					}
				}
			} else if e != nil && e != ErrNotFound {
				return e
			}
		}
		if !allowed {
			return fmt.Errorf("review location was not selected by this human context")
		}
	}
	return nil
}
