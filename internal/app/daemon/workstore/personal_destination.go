package workstore

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PersonalDestination struct{ Tenant, ChatID string }

// A private context retains its authenticated return address across attempts.
// This reads its own source records, never the user's latest navigation or a
// different project's current surface.
func (t *Tx) GetPersonalContextDestination(scopeID, contextID, roleID string) (PersonalDestination, error) {
	var destination PersonalDestination
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return destination, err
	}
	wc, err := t.GetContext(contextID)
	if err != nil {
		return destination, err
	}
	if scope.Kind != work.ScopePersonal || scope.RoleID != roleID || wc.ScopeID != scope.ID || wc.RoleID != roleID {
		return destination, ErrScopeDenied
	}
	binding, err := t.GetRoleBotBinding(roleID)
	if err != nil {
		return destination, err
	}
	inputs, err := t.ListInbox(scope.ID)
	if err != nil {
		return destination, err
	}
	for _, input := range inputs {
		if input.RecipientContextID != contextID || input.RecipientRoleID != roleID || input.MessageID == "" {
			continue
		}
		message, e := t.GetMessage(input.MessageID)
		if e != nil {
			return destination, e
		}
		if message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) {
			continue
		}
		tenant, chat := "", ""
		if message.PlatformKey != nil {
			for _, observed := range message.Observations {
				if observed.AppID == binding.AppID && observed.IdentityGeneration == binding.IdentityGeneration {
					tenant, chat = message.PlatformKey.Tenant, message.PlatformKey.ChatID
				}
			}
		} else if message.DerivedFrom != nil && message.DerivedFrom.Kind == "human_interaction" {
			action, e := t.GetHumanInteraction(message.DerivedFrom.ID)
			if e != nil {
				return destination, e
			}
			if action.PrincipalID == scope.PrincipalID && action.Gateway.AppID == binding.AppID && action.Gateway.BotGeneration == binding.IdentityGeneration && action.Gateway.RoleID == roleID {
				tenant, chat = action.Envelope.Tenant, action.Envelope.ChatID
			}
		}
		if chat == "" {
			continue
		}
		if destination.ChatID != "" && (destination.ChatID != chat || destination.Tenant != tenant) {
			return destination, fmt.Errorf("personal context has ambiguous authenticated destinations")
		}
		destination = PersonalDestination{Tenant: tenant, ChatID: chat}
	}
	if destination.ChatID == "" {
		return destination, fmt.Errorf("personal context has no authenticated platform destination")
	}
	return destination, nil
}
