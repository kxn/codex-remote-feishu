package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strconv"
)

// PrivateActionSource is assembled by trusted transport/use-case code, never
// decoded wholesale from a role tool request. Model paths use MessageID+RunRef;
// SDK Slash/card paths use the independently persisted command/interaction ID.
type PrivateActionSource struct {
	IngressID         string `json:",omitempty"`
	ReactionID        string `json:",omitempty"`
	MenuInteractionID string
	PersonalScopeID   string
	CommandID         string
	InteractionID     string
	MessageID         string
	RunRef            work.RunRef
}

func (t *Tx) ValidatePrivateAction(source PrivateActionSource) (work.WorkScope, work.SourceRef, error) {
	scope, err := t.GetScope(source.PersonalScopeID)
	if err != nil {
		return scope, work.SourceRef{}, err
	}
	if scope.Kind != work.ScopePersonal {
		return scope, work.SourceRef{}, ErrScopeDenied
	}
	count := 0
	for _, id := range []string{source.CommandID, source.InteractionID, source.MessageID, source.MenuInteractionID, source.ReactionID, source.IngressID} {
		if id != "" {
			count++
		}
	}
	if count != 1 {
		return scope, work.SourceRef{}, fmt.Errorf("one actual human action source required")
	}

	if source.IngressID != "" {
		raw, e := t.GetIngress(source.IngressID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if raw.State != work.IngressProcessed || raw.Route.ScopeID != scope.ID || raw.Envelope.ChatType != "p2p" || raw.Envelope.SenderType != "user" || raw.Envelope.ParentID == "" || raw.MessageID == "" || raw.Gateway.RoleID != scope.RoleID {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		binding, e := t.GetRoleBotBinding(scope.RoleID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if binding.GatewayRef != raw.Gateway.GatewayRef || binding.AppID != raw.Gateway.AppID || binding.IdentityGeneration != raw.Gateway.BotGeneration {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		message, e := t.GetMessage(raw.MessageID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if message.ScopeID != scope.ID || message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) || message.RecalledEvidenceRef != "" {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		recalled, e := t.IsPlatformMessageRecalled(raw.Envelope.Tenant, raw.Envelope.ChatID, raw.Envelope.MessageID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if recalled {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		return scope, work.SourceRef{Kind: "private_reply", ID: raw.ID, Version: strconv.FormatInt(raw.Revision, 10), EvidenceRef: raw.ID}, nil
	}
	if source.ReactionID != "" {
		actual, proof, e := t.ValidatePrivateReactionAction(source.ReactionID)
		if e != nil {
			return scope, proof, e
		}
		if actual.ID != scope.ID {
			return scope, proof, ErrScopeDenied
		}
		return scope, proof, nil
	}
	if source.MenuInteractionID != "" {
		menu, e := t.GetMenuInteraction(source.MenuInteractionID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if menu.ScopeID != scope.ID || menu.PrincipalID != scope.PrincipalID || menu.Gateway.RoleID != scope.RoleID {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		binding, e := t.GetRoleBotBinding(scope.RoleID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if binding.GatewayRef != menu.Gateway.GatewayRef || binding.AppID != menu.Gateway.AppID || binding.IdentityGeneration != menu.Gateway.BotGeneration {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		return scope, work.SourceRef{Kind: "sdk_menu", ID: menu.ID, Version: strconv.FormatInt(menu.Revision, 10), EvidenceRef: menu.ID}, nil
	}
	if source.CommandID != "" {
		command, e := t.GetPrivateCommand(source.CommandID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if command.ScopeID != scope.ID || command.PrincipalID != scope.PrincipalID || command.RoleID != scope.RoleID || (command.State != "sending" && command.State != "handled") {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		raw, e := t.GetIngress(command.IngressID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		recalled, e := t.IsPlatformMessageRecalled(raw.Envelope.Tenant, raw.Envelope.ChatID, raw.Envelope.MessageID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if recalled {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		if raw.Envelope.ChatType != "p2p" || raw.Envelope.SenderType != "user" || raw.Gateway.RoleID != scope.RoleID {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		principal, _, e := t.ResolvePrincipal(work.IdentityEvidence{Tenant: raw.Envelope.Tenant, AppID: raw.Envelope.AppID, Generation: raw.Gateway.BotGeneration, UnionDomain: raw.Gateway.UnionDomain, IDs: raw.Envelope.SenderIDs, ObservationRef: raw.ID})
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if principal.ID != scope.PrincipalID {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		binding, e := t.GetRoleBotBinding(scope.RoleID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if binding.IdentityGeneration != raw.Gateway.BotGeneration || binding.AppID != raw.Gateway.AppID {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		return scope, work.SourceRef{Kind: "private_command", ID: command.ID, Version: strconv.FormatInt(command.Revision, 10), EvidenceRef: raw.ID}, nil
	}
	if source.InteractionID != "" {
		interaction, e := t.GetHumanInteraction(source.InteractionID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if viewID, _ := interaction.Envelope.Values["team_view"].(string); viewID != "" {
			view, e := t.GetUIView(viewID)
			if e != nil {
				return scope, work.SourceRef{}, e
			}
			if e = t.ValidateViewInteraction(view, interaction); e != nil {
				return scope, work.SourceRef{}, e
			}
		}
		if interaction.PrincipalID != scope.PrincipalID || interaction.Gateway.RoleID != scope.RoleID {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		binding, e := t.GetRoleBotBinding(scope.RoleID)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if binding.IdentityGeneration != interaction.Gateway.BotGeneration || binding.AppID != interaction.Gateway.AppID {
			return scope, work.SourceRef{}, ErrScopeDenied
		}
		return scope, work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: strconv.FormatInt(interaction.Revision, 10), EvidenceRef: interaction.ID}, nil
	}
	attempt, err := t.GetAttemptByRunRef(source.RunRef.RunID)
	if err != nil {
		return scope, work.SourceRef{}, err
	}
	if attempt.RunRef != source.RunRef || attempt.ScopeID != scope.ID || attempt.State != work.AttemptRunning {
		return scope, work.SourceRef{}, ErrScopeDenied
	}
	worker, err := t.GetWorker(source.RunRef.WorkerID)
	if err != nil {
		return scope, work.SourceRef{}, err
	}
	if err = work.ValidateAttemptBinding(attempt, worker); err != nil {
		return scope, work.SourceRef{}, err
	}
	context, lookup := t.GetContext(worker.ContextID)
	if lookup != nil {
		return scope, work.SourceRef{}, lookup
	}
	if context.Purpose == work.PurposeReview {
		return scope, work.SourceRef{}, ErrScopeDenied
	}
	if worker.RoleID != scope.RoleID {
		return scope, work.SourceRef{}, ErrScopeDenied
	}
	for _, id := range attempt.InputIDs {
		input, e := t.GetInbox(id)
		if e != nil {
			return scope, work.SourceRef{}, e
		}
		if input.MessageID == source.MessageID && input.State == work.InboxClaimed && input.ClaimedAttemptID == attempt.ID {
			message, e := t.GetMessage(source.MessageID)
			if e != nil {
				return scope, work.SourceRef{}, e
			}
			if message.RecalledEvidenceRef != "" || message.ScopeID != scope.ID || message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) {
				return scope, work.SourceRef{}, ErrScopeDenied
			}
			return scope, work.SourceRef{Kind: "message", ID: message.ID, Version: strconv.FormatInt(message.Revision, 10), EvidenceRef: message.BodyRef}, nil
		}
	}
	return scope, work.SourceRef{}, fmt.Errorf("human navigation source was not admitted to this run")
}
