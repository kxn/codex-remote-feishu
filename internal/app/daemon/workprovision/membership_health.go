package workprovision

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// CheckMembershipHealth is a read-only platform probe followed by a local
// observation transaction. Call at a bounded cadence (daemon: two minutes),
// not every scheduler tick. It never invites, removes, or starts any bot/worker.
func (s *Service) CheckMembershipHealth(ctx context.Context, projectID string) error {
	var channel work.ProjectChannelBinding
	var bindings []work.RoleBotBinding
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		project, err := tx.GetProject(projectID)
		if err != nil {
			return err
		}
		if project.State == work.ProjectArchived {
			return nil
		}
		channels, err := tx.ListProjectChannelBindings(projectID)
		if err != nil {
			return err
		}
		for _, candidate := range channels {
			if candidate.Active && candidate.Purpose == "main" {
				channel = candidate
				break
			}
		}
		if channel.ChatID == "" {
			return fmt.Errorf("project has no active primary channel")
		}
		members, err := tx.ListProjectMembers(projectID)
		if err != nil {
			return err
		}
		for _, member := range members {
			if member.State != work.ProjectMemberReady {
				continue
			}
			binding, err := tx.GetRoleBotBinding(member.RoleID)
			if err != nil {
				return err
			}
			if binding.IdentityGeneration != member.BotGeneration {
				continue
			}
			bindings = append(bindings, binding)
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, binding := range bindings {
		platform, err := s.platform(ctx, binding)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		present, err := platform.IsCallerInChat(ctx, channel.ChatID)
		if err != nil {
			failures = append(failures, fmt.Errorf("membership probe: %w", err))
			continue
		}
		if present {
			continue
		}
		observed := workstore.ProjectMembershipObservation{ProjectID: projectID, Binding: binding, ChannelGeneration: channel.BindingGeneration, Tenant: channel.Tenant, ChatID: channel.ChatID, Present: false, ObservedAt: s.now().UTC()}
		err = s.store.Within(ctx, func(tx *workstore.Tx) error {
			source, err := tx.RecordProjectMembershipProbe(stableID("membership-probe", projectID, binding.RoleID, observed.ObservedAt.Format(time.RFC3339Nano)), observed)
			if err != nil {
				return err
			}
			observed.Source = source
			return tx.ApplyProjectMembershipObservation(observed)
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
