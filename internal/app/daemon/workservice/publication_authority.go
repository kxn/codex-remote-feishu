package workservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

var ErrPublicationDenied = errors.New("publication is not authorized")

type PublicationPurpose string

const (
	PublicationProgress PublicationPurpose = "progress"
	PublicationFinal    PublicationPurpose = "final"
	PublicationReply    PublicationPurpose = "reply"
)

type publicationKind string

const (
	publicationMessage  publicationKind = "message"
	publicationDelivery publicationKind = "delivery"
	publicationMedia    publicationKind = "media"
)

const (
	publicationPrivate   = "private"
	publicationBootstrap = "bootstrap"
	publicationLeader    = "leader"
	publicationDirected  = "directed"
	publicationDelegated = "delegated"
)

// PublicationAuthority is a daemon-generated snapshot applied to an outbox
// operation. It never comes from the CLI payload.
type PublicationAuthority struct {
	LeadershipGeneration int64                           `json:"leadership_generation,omitempty"`
	Kind                 string                          `json:"kind"`
	Content              publicationKind                 `json:"content"`
	AttemptID            string                          `json:"attempt_id"`
	InputID              string                          `json:"input_id,omitempty"`
	GrantID              string                          `json:"grant_id,omitempty"`
	GrantRevision        int64                           `json:"grant_revision,omitempty"`
	Participant          work.PublicationMemberAuthority `json:"participant,omitempty"`
	TargetTask           work.PublicationTaskAuthority   `json:"target_task,omitempty"`
	References           []work.DeliveryRef              `json:"references,omitempty"`
}

func (s *Service) authorizePublication(tx *workstore.Tx, c Caller, replyID, taskID, grantID string, kind publicationKind, refs []work.DeliveryRef) (MessageProjection, error) {
	var projection MessageProjection
	scope, err := tx.GetScope(c.Worker.ScopeID)
	if err != nil {
		return projection, err
	}
	wc, err := tx.GetContext(c.Worker.ContextID)
	if err != nil {
		return projection, err
	}
	if wc.Purpose == work.PurposeReview {
		return projection, ErrPublicationDenied
	}
	authority := &PublicationAuthority{AttemptID: c.Attempt.ID, Content: kind, InputID: replyID}
	switch scope.Kind {
	case work.ScopePersonal:
		if grantID != "" {
			return projection, ErrPublicationDenied
		}
		authority.Kind = publicationPrivate
	case work.ScopeBootstrap:
		if grantID != "" || kind != publicationMessage {
			return projection, ErrPublicationDenied
		}
		authority.Kind = publicationBootstrap
	case work.ScopeProject:
		if kind == publicationMedia {
			return projection, ErrPublicationDenied
		}
		project, e := tx.GetProject(scope.ProjectID)
		if e != nil {
			return projection, e
		}
		if project.State == work.ProjectArchiving || project.State == work.ProjectArchived {
			return projection, ErrPublicationDenied
		}
		authority.Participant, e = publicationMember(tx, scope, c.Worker.RoleID)
		if e != nil {
			return projection, e
		}
		switch {
		case grantID != "":
			authority.Kind = publicationDelegated
		case project.LeaderRoleID == c.Worker.RoleID:
			authority.Kind = publicationLeader
			authority.LeadershipGeneration = project.LeadershipGeneration()
			if replyID == "" && taskID == "" {
				authority.InputID, e = currentPlatformReply(tx, c)
				if e != nil {
					return projection, e
				}
			}
		case kind == publicationMessage && replyID != "":
			authority.Kind = publicationDirected
			input, e := tx.GetInbox(replyID)
			if e != nil {
				return projection, publicationRecordError(e)
			}
			if taskID != "" && taskID != input.TaskID && taskID != input.RelatedTaskID {
				return projection, ErrPublicationDenied
			}
		default:
			return projection, fmt.Errorf("%w: send the result internally to the project leader", ErrPublicationDenied)
		}
	default:
		return projection, ErrPublicationDenied
	}

	projection, err = s.messageDestination(tx, c, authority.InputID, taskID)
	if err != nil {
		return projection, err
	}
	if scope.Kind == work.ScopePersonal {
		projection.SourceRunRef = c.Attempt.RunRef.RunID
	}
	if projection.TaskID != "" {
		task, e := tx.GetTask(projection.TaskID)
		if e != nil {
			return projection, e
		}
		authority.TargetTask = taskAuthority(task)
		if e = validatePublicationTask(tx, scope, authority.TargetTask); e != nil {
			return projection, e
		}
	}
	if authority.Kind == publicationDirected {
		if _, e := validateDirectedReply(tx, scope, c.Attempt.ID, c.Worker.RoleID, authority.InputID, projection); e != nil {
			return projection, e
		}
	}
	if authority.Kind == publicationDelegated {
		grant, e := tx.GetPublicationGrant(scope.ID, grantID)
		if e != nil {
			return projection, publicationRecordError(e)
		}
		authority.GrantID, authority.GrantRevision = grant.ID, grant.Revision
		authority.References = append([]work.DeliveryRef(nil), refs...)
		if err = validateGrantPublication(tx, scope, grant, *authority, projection); err != nil {
			return projection, err
		}
	}
	projection.Authority = authority
	return projection, nil
}

func currentPlatformReply(tx *workstore.Tx, c Caller) (string, error) {
	var reply string
	for _, id := range c.Attempt.InputIDs {
		input, err := tx.GetInbox(id)
		if err != nil {
			return "", err
		}
		if input.ClaimedAttemptID != c.Attempt.ID || input.MessageID == "" {
			continue
		}
		message, err := tx.GetMessage(input.MessageID)
		if err != nil {
			return "", err
		}
		if message.Source.Kind != work.ActorPrincipal || message.PlatformKey == nil {
			continue
		}
		if reply != "" {
			return "", nil
		}
		reply = id
	}
	return reply, nil
}

func setPublicationPurpose(projection *MessageProjection, purpose PublicationPurpose, body string) error {
	authority := projection.Authority
	if authority.Kind == publicationDirected {
		if purpose != "" && purpose != PublicationReply {
			return fmt.Errorf("%w: this input authorizes a direct reply", ErrPublicationDenied)
		}
		purpose = PublicationReply
	}
	if authority.Kind == publicationDelegated && purpose == "" {
		purpose = PublicationFinal
	}
	if purpose == "" {
		switch {
		case authority.InputID != "", authority.Kind == publicationPrivate, authority.Kind == publicationBootstrap:
			purpose = PublicationReply
		default:
			purpose = PublicationProgress
		}
	}
	switch purpose {
	case PublicationReply:
		if authority.InputID == "" && authority.Kind != publicationPrivate && authority.Kind != publicationBootstrap {
			return fmt.Errorf("%w: a reply needs an input admitted to this run", ErrPublicationDenied)
		}
	case PublicationFinal, PublicationProgress:
	default:
		return publicationArgument("purpose must be reply, progress, or final")
	}
	if purpose == PublicationProgress && authority.Kind != publicationPrivate {
		if _, err := GroupProgressCard(body); err != nil {
			return err
		}
	} else if len(body) > 24*1024 {
		return publicationArgument("Publication exceeds the text limit. Send a concise message or deliver a saved result.")
	}
	projection.Purpose, projection.Body = purpose, body
	return nil
}

func publicationRecordError(err error) error {
	if errors.Is(err, workstore.ErrNotFound) || errors.Is(err, workstore.ErrScopeDenied) {
		return ErrPublicationDenied
	}
	return err
}

func publicationMember(tx *workstore.Tx, scope work.WorkScope, roleID string) (work.PublicationMemberAuthority, error) {
	var result work.PublicationMemberAuthority
	team, err := tx.GetTeamMember(scope.TeamID, roleID)
	if err != nil {
		return result, publicationRecordError(err)
	}
	member, err := tx.GetProjectMember(scope.ProjectID, roleID)
	if err != nil {
		return result, publicationRecordError(err)
	}
	bot, err := tx.GetRoleBotBinding(roleID)
	if err != nil {
		return result, publicationRecordError(err)
	}
	if !team.Active || member.State != work.ProjectMemberReady || member.BotGeneration != bot.IdentityGeneration {
		return result, ErrPublicationDenied
	}
	return work.PublicationMemberAuthority{RoleID: roleID, TeamRevision: team.Revision, ProjectRevision: member.Revision, BotGeneration: bot.IdentityGeneration}, nil
}

func taskAuthority(task work.Task) work.PublicationTaskAuthority {
	return work.PublicationTaskAuthority{TaskID: task.ID, OwnerRoleID: task.OwnerRoleID, RequirementsRevision: task.RequirementsRevision, AssignmentGeneration: task.AssignmentGeneration}
}

func validateDirectedReply(tx *workstore.Tx, scope work.WorkScope, attemptID, roleID, inputID string, dest MessageProjection) (work.Inbox, error) {
	var input work.Inbox
	ids, err := tx.EffectiveAttemptInputIDs(attemptID)
	if err != nil {
		return input, publicationRecordError(err)
	}
	if inputID == "" || !slices.Contains(ids, inputID) {
		return input, ErrPublicationDenied
	}
	input, err = tx.GetInbox(inputID)
	if err != nil {
		return input, publicationRecordError(err)
	}
	if input.ScopeID != scope.ID || input.RecipientRoleID != roleID || input.ClaimedAttemptID != attemptID ||
		input.DirectedReplyIngressID == "" || input.DirectedReplyBotGeneration != dest.BotGeneration {
		return input, ErrPublicationDenied
	}
	message, err := tx.GetMessage(input.MessageID)
	if err != nil {
		return input, publicationRecordError(err)
	}
	key := work.MessageKey{Tenant: dest.Tenant, ChatID: dest.ChatID, MessageID: dest.ReplyToMessageID}
	if message.Source.Kind != work.ActorPrincipal || message.PlatformKey == nil || *message.PlatformKey != key {
		return input, ErrPublicationDenied
	}
	mapping, err := tx.GetPlatformMessageMapping(key.Tenant, key.ChatID, key.MessageID)
	if err != nil {
		return input, publicationRecordError(err)
	}
	if mapping.ScopeID != scope.ID || mapping.IngressID != input.DirectedReplyIngressID || mapping.RootMessageID != dest.RootMessageID || mapping.ChannelGeneration != dest.ChannelGeneration {
		return input, ErrPublicationDenied
	}
	ingress, err := tx.GetIngress(mapping.IngressID)
	if err != nil {
		return input, publicationRecordError(err)
	}
	if ingress.Envelope.SenderType != "user" || ingress.State != work.IngressProcessed || ingress.Route.ScopeID != scope.ID || ingress.Route.BindingGeneration != dest.ChannelGeneration {
		return input, ErrPublicationDenied
	}
	recalled, err := tx.IsPlatformMessageRecalled(key.Tenant, key.ChatID, key.MessageID)
	if recalled {
		return input, ErrPublicationDenied
	}
	return input, err
}

func GroupProgressCard(body string) (json.RawMessage, error) {
	card, err := json.Marshal(map[string]any{
		"schema": "2.0",
		"config": map[string]any{"update_multi": true},
		"body":   map[string]any{"elements": []any{map[string]any{"tag": "markdown", "content": body}}},
	})
	if err != nil {
		return nil, err
	}
	if len(card) > 28*1024 {
		return nil, publicationArgument("Progress exceeds the card limit. Send a concise progress summary.")
	}
	return card, nil
}

func publicationArgument(message string) error {
	return &workcmd.Error{Code: "invalid_argument", Message: message}
}
