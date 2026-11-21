package workstore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ProjectMembershipObservation struct {
	ProjectID         string              `json:"project_id"`
	Binding           work.RoleBotBinding `json:"binding"`
	ChannelGeneration int64               `json:"channel_generation"`
	Tenant            string              `json:"tenant"`
	ChatID            string              `json:"chat_id"`
	Present           bool                `json:"present"`
	ChatDeleted       bool                `json:"chat_deleted"`
	Source            work.SourceRef      `json:"source"`
	ObservedAt        time.Time           `json:"observed_at"`
}

// RecordProjectMembershipProbe is a trusted adapter-observation entry, never a
// CLI command. Only a completed IsCallerInChat read may supply Present=false;
// transport errors must not be recorded as absence.
func (t *Tx) RecordProjectMembershipProbe(id string, v ProjectMembershipObservation) (work.SourceRef, error) {
	project, err := t.GetProject(v.ProjectID)
	if err != nil {
		return work.SourceRef{}, err
	}
	if id == "" || v.ObservedAt.IsZero() || v.ChatDeleted {
		return work.SourceRef{}, fmt.Errorf("membership probe requires actual read evidence")
	}
	v.Source = work.SourceRef{}
	raw, err := json.Marshal(v)
	if err != nil {
		return work.SourceRef{}, err
	}
	content := work.NewContent(id, project.ScopeID, "application/vnd.codex.membership-probe+json", raw, v.ObservedAt)
	if err = t.PutContent(content); err != nil {
		return work.SourceRef{}, err
	}
	return work.SourceRef{Kind: "membership_probe", ID: id, Version: content.SHA256, EvidenceRef: id}, nil
}

// ApplyProjectMembershipObservation preserves independent human pause gates and
// user-selected roster state. A positive observation never automatically marks
// an unavailable member ready; provisioning must confirm it under its own CAS.
func (t *Tx) ApplyProjectMembershipObservation(v ProjectMembershipObservation) error {
	return t.atomic(func() error {
		project, err := t.GetProject(v.ProjectID)
		if err != nil {
			return err
		}
		binding, err := t.GetProjectChannelBinding(v.Tenant, v.ChatID)
		if err != nil {
			return err
		}
		if !binding.Active || binding.ProjectID != project.ID || binding.BindingGeneration != v.ChannelGeneration {
			return ErrConflict
		}
		actual, err := t.GetRoleBotBinding(v.Binding.RoleID)
		if err != nil {
			return err
		}
		if actual != v.Binding {
			return ErrConflict
		}
		if err = v.Source.Validate(); err != nil {
			return err
		}
		observedAt := v.ObservedAt
		switch v.Source.Kind {
		case "platform_observation":
			var raw string
			if err = t.tx.QueryRowContext(t.ctx, "SELECT payload FROM team_platform_observations WHERE id=?", v.Source.ID).Scan(&raw); err != nil {
				return err
			}
			var source PlatformObservation
			if err = json.Unmarshal([]byte(raw), &source); err != nil {
				return err
			}
			if source.Tenant != v.Tenant || source.ChatID != v.ChatID || source.Gateway.RoleID != v.Binding.RoleID || source.Gateway.AppID != v.Binding.AppID || source.Gateway.GatewayRef != v.Binding.GatewayRef || source.Gateway.BotGeneration != v.Binding.IdentityGeneration || v.Source.EvidenceRef != source.ID || v.Source.Version != "1" {
				return ErrScopeDenied
			}
			if source.Kind != "bot.removed" && source.Kind != "chat.deleted" {
				return fmt.Errorf("platform observation does not prove absence")
			}
			if v.Present || v.ChatDeleted != (source.Kind == "chat.deleted") {
				return fmt.Errorf("platform observation facts mismatch")
			}
			observedAt = source.ReceivedAt
		case "membership_probe":
			content, err := t.GetContent(project.ScopeID, v.Source.EvidenceRef)
			if err != nil {
				return err
			}
			if content.ID != v.Source.ID || content.SHA256 != v.Source.Version || content.MediaType != "application/vnd.codex.membership-probe+json" {
				return ErrScopeDenied
			}
			var source ProjectMembershipObservation
			if err = json.Unmarshal(content.Data, &source); err != nil {
				return err
			}
			if source.ProjectID != v.ProjectID || source.Binding != v.Binding || source.ChannelGeneration != v.ChannelGeneration || source.Tenant != v.Tenant || source.ChatID != v.ChatID || source.Present != v.Present || source.ChatDeleted {
				return ErrScopeDenied
			}
			observedAt = source.ObservedAt
		default:
			return fmt.Errorf("membership requires actual SDK or membership-probe evidence")
		}
		if observedAt.IsZero() {
			return fmt.Errorf("membership observation time required")
		}
		if v.Present {
			return nil
		}
		members, err := t.ListProjectMembers(project.ID)
		if err != nil {
			return err
		}
		changed := false
		for _, member := range members {
			if !v.ChatDeleted && member.RoleID != v.Binding.RoleID {
				continue
			}
			if member.State == work.ProjectMemberRemoved || member.State == work.ProjectMemberUnavailable {
				continue
			}
			memberBinding, err := t.GetRoleBotBinding(member.RoleID)
			if err != nil {
				return err
			}
			if member.BotGeneration != memberBinding.IdentityGeneration {
				continue
			}
			member.State = work.ProjectMemberUnavailable
			member.AvailabilityEvidence = &v.Source
			expected := member.Revision
			member.Revision++
			if err = t.PutProjectMember(member, expected); err != nil {
				return err
			}
			changed = true
		}
		if v.ChatDeleted {
			gateID := "chat-deleted:" + v.Source.ID
			if _, err := t.GetPauseGate(gateID); err == ErrNotFound {
				if err = t.PutPauseGate(work.PauseGate{ID: gateID, ScopeID: project.ScopeID, Source: work.GateReconciliation, SourceID: v.Source.ID, State: work.GateClosed, Revision: 1}, 0); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		if changed && project.State == work.ProjectActive {
			expected := project.Revision
			project.Revision++
			project.State = work.ProjectDegraded
			return t.PutProject(project, expected)
		}
		return nil
	})
}
