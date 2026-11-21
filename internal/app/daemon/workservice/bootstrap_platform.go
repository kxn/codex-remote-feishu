package workservice

import (
	"context"
	"errors"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type BootstrapPlatform struct {
	ConfiguredApps      func() (map[string]string, error)
	RegisterLocations   func(context.Context) error
	QueueLocationPicker func(*workstore.Tx, work.WorkScope, work.Message, string) (string, error)
	VerifyMembership    func(context.Context, string, string, string, work.SenderIDs, work.IngressGateway) error
}

func (s *Service) bootstrapConfiguredApps() (map[string]string, error) {
	if s.bootstrapPlatform.ConfiguredApps == nil {
		return nil, nil
	}
	return s.bootstrapPlatform.ConfiguredApps()
}

func bootstrapAppEnabled(apps map[string]string, binding work.RoleBotBinding) bool {
	return apps == nil || apps[binding.GatewayRef] == binding.AppID
}

func (s *Service) WithBootstrapPlatform(platform BootstrapPlatform) *Service {
	s.bootstrapPlatform = platform
	return s
}

type BootstrapPickLocationRequest struct {
	Key             string `json:"key"`
	SourceMessageID string `json:"source_message_id"`
}

func (s *Service) bootstrapSource(ctx context.Context, token, run, sourceID string, apply func(*workstore.Tx, Caller, work.WorkScope, work.Message) error) error {
	claims, err := s.signer.Verify(token)
	if err != nil {
		return err
	}
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		caller, scope, err := s.authorizeBootstrapWorker(tx, claims, run)
		if err != nil {
			return err
		}
		if err = tx.RequireOpenBootstrapScope(scope); err != nil {
			return err
		}
		message, _, err := currentHumanInstruction(tx, caller, sourceID)
		if err != nil {
			return err
		}
		if message.Source.ID != scope.PrincipalID || message.PlatformKey == nil || message.PlatformKey.Tenant != scope.Tenant || message.PlatformKey.ChatID != scope.ChatID {
			return workauth.ErrUnauthorized
		}
		return apply(tx, caller, scope, message)
	})
}

func (s *Service) BootstrapPickLocation(ctx context.Context, token, run string, req BootstrapPickLocationRequest) (Result, error) {
	var result Result
	if s.bootstrapPlatform.QueueLocationPicker == nil || s.bootstrapPlatform.RegisterLocations == nil {
		return result, &workcmd.Error{Code: "unavailable", Message: "execution directory chooser is unavailable"}
	}
	if err := s.bootstrapSource(ctx, token, run, req.SourceMessageID, func(*workstore.Tx, Caller, work.WorkScope, work.Message) error { return nil }); err != nil {
		return result, err
	}
	if err := s.bootstrapPlatform.RegisterLocations(ctx); err != nil {
		return result, err
	}
	err := s.bootstrapSource(ctx, token, run, req.SourceMessageID, func(tx *workstore.Tx, caller Caller, scope work.WorkScope, message work.Message) error {
		var err error
		result, err = s.mutate(tx, caller, "bootstrap.location.pick", req.Key, scope.ID, req, func() (string, int64, error) {
			id, e := s.bootstrapPlatform.QueueLocationPicker(tx, scope, message, req.Key)
			return id, 1, e
		})
		return err
	})
	if err == nil {
		result.Status = "accepted"
	}
	return result, err
}

type BootstrapResolveTeamRequest struct {
	ProjectName     string `json:"project_name"`
	SourceMessageID string `json:"source_message_id"`
}

type BootstrapProjectTeam struct {
	ProjectName string `json:"project_name"`
	TeamID      string `json:"team_id"`
}

func (s *Service) BootstrapResolveTeam(ctx context.Context, token, run string, req BootstrapResolveTeamRequest) ([]BootstrapProjectTeam, error) {
	matches := []BootstrapProjectTeam{}
	name := strings.TrimSpace(req.ProjectName)
	var source work.IngressRecord
	var principalID string
	var candidates []work.Project
	err := s.bootstrapSource(ctx, token, run, req.SourceMessageID, func(tx *workstore.Tx, caller Caller, scope work.WorkScope, message work.Message) error {
		content, e := tx.GetContent(scope.ID, message.BodyRef)
		if e != nil {
			return e
		}
		if name == "" || !strings.Contains(string(content.Data), name) {
			return &workcmd.Error{Code: "invalid_argument", Message: "the exact project name must appear in the human source message"}
		}
		key := message.PlatformKey
		principalID = scope.PrincipalID
		source, e = tx.GetIngressByPlatformMessage(key.Tenant, key.ChatID, key.MessageID)
		if e != nil {
			return e
		}
		projects, e := tx.ListProjects()
		if e != nil {
			return e
		}
		for _, project := range projects {
			if project.Name != name {
				continue
			}
			member, e := tx.GetTeamMember(project.TeamID, caller.Worker.RoleID)
			if errors.Is(e, workstore.ErrNotFound) || e == nil && !member.Active {
				continue
			}
			if e != nil {
				return e
			}
			access, e := tx.GetProjectPrincipalAccess(project.ScopeID, scope.PrincipalID)
			if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			if access.Tenant == scope.Tenant && tx.RequireProjectPrincipalAccess(project.ScopeID, scope.PrincipalID, s.now()) == nil {
				matches = append(matches, BootstrapProjectTeam{ProjectName: project.Name, TeamID: project.TeamID})
			} else {
				candidates = append(candidates, project)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, project := range candidates {
		if s.bootstrapPlatform.VerifyMembership != nil && s.bootstrapPlatform.VerifyMembership(ctx, principalID, project.ScopeID, source.Envelope.Tenant, source.Envelope.SenderIDs, source.Gateway) == nil {
			match := BootstrapProjectTeam{ProjectName: project.Name, TeamID: project.TeamID}
			if err = s.bootstrapSource(ctx, token, run, req.SourceMessageID, func(tx *workstore.Tx, caller Caller, scope work.WorkScope, _ work.Message) error {
				if e := tx.RequireProjectPrincipalAccess(project.ScopeID, scope.PrincipalID, s.now()); e != nil {
					return e
				}
				current, e := tx.GetProject(project.ID)
				if e != nil {
					return e
				}
				member, e := tx.GetTeamMember(current.TeamID, caller.Worker.RoleID)
				if e == nil && member.Active && current.Name == match.ProjectName && current.TeamID == match.TeamID {
					matches = append(matches, match)
				}
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}
	return matches, ctx.Err()
}
