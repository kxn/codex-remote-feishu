package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type CreateProjectRequest struct {
	Key                       string   `json:"key"`
	Tenant                    string   `json:"tenant"`
	ChatID                    string   `json:"chat_id"`
	PrincipalID               string   `json:"principal_id"`
	ExpectedBindingGeneration int64    `json:"expected_binding_generation"`
	Name                      string   `json:"name"`
	TeamID                    string   `json:"team_id"`
	LeaderRoleID              string   `json:"leader_role_id"`
	MemberRoleIDs             []string `json:"member_role_ids"`
	// These are previously selected resources, not agent-invented directories.
	LocationIDs       []string `json:"location_ids"`
	DefaultLocationID string   `json:"default_location_id,omitempty"`
	DocumentContainer string   `json:"document_container,omitempty"`
	InitialRequest    string   `json:"initial_request"`
	SourceMessageID   string   `json:"source_message_id,omitempty"`
}

// CreateProject reserves the channel and stores the entire provisioning intent.
// A committed reservation is accepted, not a claim that remote setup finished.
func (s *Service) CreateProject(ctx context.Context, token string, req CreateProjectRequest) (Result, error) {
	return s.CreateProjectInRun(ctx, token, "", req)
}
func (s *Service) CreateProjectInRun(ctx context.Context, token, runRef string, req CreateProjectRequest) (Result, error) {
	claims, err := s.signer.Verify(token)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.InitialRequest) == "" || req.Key == "" {
		return Result{}, fmt.Errorf("project name, initial request and key are required")
	}
	if claims.Purpose == "bootstrap" && req.SourceMessageID == "" {
		req.SourceMessageID = stableID("form_input", claims.Tenant, claims.ChatID, claims.OperationID)
	}
	apps, err := s.bootstrapConfiguredApps()
	if err != nil {
		return Result{}, err
	}
	var result Result
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		binding, err := tx.GetRoleBotBinding(claims.RoleID)
		if err != nil {
			return workauth.ErrUnauthorized
		}
		if claims.Purpose == "bootstrap" {
			if runRef != "" {
				return workauth.ErrUnauthorized
			}
			if err = workauth.AuthorizeBootstrap(claims, req.Tenant, req.ChatID, req.PrincipalID, req.Key, binding); err != nil {
				return err
			}
		} else {
			caller, scope, err := s.authorizeBootstrapWorker(tx, claims, runRef)
			if err != nil {
				return err
			}
			if scope.Tenant != req.Tenant || scope.ChatID != req.ChatID || scope.PrincipalID != req.PrincipalID || scope.BootstrapOperationID != req.Key || scope.BindingGeneration != req.ExpectedBindingGeneration {
				return workauth.ErrUnauthorized
			}
			admitted := false
			for _, id := range caller.Attempt.InputIDs {
				input, e := tx.GetInbox(id)
				if e != nil {
					return e
				}
				if input.MessageID == req.SourceMessageID {
					admitted = true
				}
			}
			if !admitted || req.SourceMessageID == "" {
				return workauth.ErrUnauthorized
			}
			source, e := tx.GetMessage(req.SourceMessageID)
			if e != nil {
				return e
			}
			if source.ScopeID != scope.ID || source.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) {
				return workauth.ErrUnauthorized
			}
			if source.RecalledEvidenceRef != "" {
				return workauth.ErrUnauthorized
			}
			if req.DocumentContainer != "" {
				if err = validateSelectedDirectoryURL(req.DocumentContainer); err != nil {
					return err
				}
				if err = directorySourceContainsURL(tx, source, req.DocumentContainer); err != nil {
					return err
				}
			}
		}
		if req.LeaderRoleID != claims.RoleID {
			return workauth.ErrUnauthorized
		}
		choiceSource, err := s.projectExecutionChoice(tx, claims, &req)
		if err != nil {
			return err
		}
		if _, err = tx.GetPrincipal(req.PrincipalID); err != nil {
			return err
		}
		projectID := stableID("project", req.Tenant, req.ChatID, req.Key)
		scopeID := stableID("scope", projectID)
		opID := stableID("op", projectID, "provision")
		body, err := json.Marshal(req)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		requestHash := hex.EncodeToString(hash[:])
		if existing, e := tx.GetOperation(opID); e == nil {
			if existing.RequestHash != requestHash || existing.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: req.PrincipalID}) {
				return workstore.ErrIdempotencyConflict
			}
			result = Result{Status: "accepted", OperationID: opID, ObjectID: projectID, Revision: 1, Replay: true}
			if existing.State == work.OperationConfirmed {
				result.Status = "completed"
			}
			return nil
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		currentChannel, channelErr := tx.GetProjectChannelBinding(req.Tenant, req.ChatID)
		if channelErr == nil && currentChannel.Active {
			// A CAS number is not consent to replace a group's existing project.
			// Rebinding is a separate, explicitly authorized lifecycle operation.
			return workstore.ErrConflict
		}
		if channelErr != nil && !errors.Is(channelErr, workstore.ErrNotFound) {
			return channelErr
		}
		if _, err = tx.GetTeam(req.TeamID); err != nil {
			return err
		}
		selected := map[string]bool{req.LeaderRoleID: true}
		for _, id := range req.MemberRoleIDs {
			if id == "" {
				return fmt.Errorf("empty selected role")
			}
			selected[id] = true
		}
		for roleID := range selected {
			member, e := tx.GetTeamMember(req.TeamID, roleID)
			if e != nil || !member.Active {
				return fmt.Errorf("selected role is not an active team member")
			}
			bot, e := tx.GetRoleBotBinding(roleID)
			if e != nil {
				return fmt.Errorf("selected role has no configured bot: %w", e)
			}
			if !bootstrapAppEnabled(apps, bot) {
				return fmt.Errorf("selected role %q no longer has an enabled Bot", roleID)
			}
		}
		for _, id := range req.LocationIDs {
			if _, err = tx.GetLocation(id); err != nil {
				return err
			}
		}
		if claims.Purpose == "bootstrap" {
			if err = s.ensureFormSource(tx, claims, req); err != nil {
				return err
			}
		}
		if err = tx.PutScope(work.WorkScope{ID: scopeID, Kind: work.ScopeProject, TeamID: req.TeamID, ProjectID: projectID, Revision: 1, CreatedAt: s.now()}, 0); err != nil {
			return err
		}
		project := work.Project{ID: projectID, Name: req.Name, ScopeID: scopeID, TeamID: req.TeamID, LeaderRoleID: req.LeaderRoleID, State: work.ProjectProvisioning, Revision: 1}
		if err = tx.PutProject(project, 0); err != nil {
			return err
		}
		channel := work.ProjectChannelBinding{ProjectID: projectID, Tenant: req.Tenant, ChatID: req.ChatID, Purpose: "main", BindingGeneration: req.ExpectedBindingGeneration + 1, Active: true}
		if err = tx.PutProjectChannelBinding(channel, req.ExpectedBindingGeneration); err != nil {
			return err
		}
		for roleID := range selected {
			if err = tx.PutProjectMember(work.ProjectMember{ProjectID: projectID, RoleID: roleID, State: work.ProjectMemberInvited, Revision: 1}, 0); err != nil {
				return err
			}
		}
		content := work.NewContent(stableID("request", opID), scopeID, "application/json", body, s.now())
		if err = tx.PutContent(content); err != nil {
			return err
		}
		if len(req.LocationIDs) > 0 {
			selection := work.NewContent(stableID("execution_choice", opID), scopeID, "application/json", choiceSource, s.now())
			if err = tx.PutContent(selection); err != nil {
				return err
			}
			if err = tx.PutExecutionPreference(workstore.ExecutionPreference{ScopeID: scopeID, SelectedLocationIDs: req.LocationIDs, DefaultLocationID: req.DefaultLocationID, SelectedBy: work.Actor{Kind: work.ActorPrincipal, ID: req.PrincipalID}, SourceRef: selection.ID, Revision: 1}, 0); err != nil {
				return err
			}
		}
		op := work.Operation{ID: opID, ScopeID: scopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: req.PrincipalID}, Kind: "project.provision", TargetID: projectID, TargetGeneration: channel.BindingGeneration, IdempotencyKey: req.Key, RequestHash: requestHash, RequestRef: content.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		for _, step := range []string{"members", "materials", "navigation", "execution", "initial_input"} {
			op.Steps = append(op.Steps, work.OperationStep{ID: step, State: work.OperationPrepared})
		}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		result = Result{Status: "accepted", OperationID: opID, ObjectID: projectID, Revision: 1}
		return nil
	})
	return result, err
}
