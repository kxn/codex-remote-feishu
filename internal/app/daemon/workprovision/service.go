package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var ErrUnknown = errors.New("project provisioning effect needs explicit reconciliation")
var ErrCancelled = errors.New("project provisioning was explicitly canceled")
var ErrStale = errors.New("project provisioning binding is no longer current")

type snapshot struct {
	operation work.Operation
	project   work.Project
	request   workservice.CreateProjectRequest
	bindings  map[string]work.RoleBotBinding
	home      workstore.ProjectHome
}

func New(store *workstore.Store, options Options) (*Service, error) {
	if store == nil || options.Resolver == nil || options.Execution == nil {
		return nil, fmt.Errorf("provisioning store, identity resolver and execution preparer are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{store: store, resolver: options.Resolver, execution: options.Execution, now: options.Now}, nil
}
func (s *Service) load(ctx context.Context, operationID string) (snapshot, error) {
	var state snapshot
	state.bindings = map[string]work.RoleBotBinding{}
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		operation, err := tx.GetOperation(operationID)
		if err != nil {
			return err
		}
		if operation.Kind != "project.provision" {
			return fmt.Errorf("operation is not project provisioning")
		}
		state.operation = operation
		state.project, err = tx.GetProject(operation.TargetID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(operation.ScopeID, operation.RequestRef)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body.Data, &state.request); err != nil {
			return err
		}
		if operation.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: state.request.PrincipalID}) || state.project.ScopeID != operation.ScopeID {
			return workstore.ErrScopeDenied
		}
		if err = s.current(tx, state); err != nil {
			return err
		}
		members, err := tx.ListProjectMembers(state.project.ID)
		if err != nil {
			return err
		}
		for _, member := range members {
			if member.State == work.ProjectMemberRemoved {
				continue
			}
			binding, err := tx.GetRoleBotBinding(member.RoleID)
			if err != nil {
				return err
			}
			state.bindings[member.RoleID] = binding
		}
		home, err := tx.GetProjectHome(state.project.ID)
		if errors.Is(err, workstore.ErrNotFound) {
			binding, ok := state.bindings[state.project.LeaderRoleID]
			if !ok {
				return fmt.Errorf("project has no selected leader identity")
			}
			home = workstore.ProjectHome{ID: state.project.ID, ScopeID: state.project.ScopeID, OperationID: operation.ID, ChannelGeneration: operation.TargetGeneration, Identity: binding, SyncState: "pending", Revision: 1, UpdatedAt: s.now()}
			if err = tx.PutProjectHome(home, 0); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		state.home = home
		return s.loadDirectorySelection(tx, &state)
	})
	return state, err
}
func (s *Service) current(tx *workstore.Tx, state snapshot) error {
	binding, err := tx.GetProjectChannelBinding(state.request.Tenant, state.request.ChatID)
	if err != nil {
		return err
	}
	if !binding.Active || binding.ProjectID != state.project.ID || binding.BindingGeneration != state.operation.TargetGeneration {
		return ErrStale
	}
	if state.home.ID != "" {
		home, e := tx.GetProjectHome(state.project.ID)
		if e != nil {
			return e
		}
		if home.DirectoryGeneration != state.home.DirectoryGeneration {
			return ErrStale
		}
	}
	return nil
}
func (s *Service) platform(ctx context.Context, binding work.RoleBotBinding) (Platform, error) {
	var current work.RoleBotBinding
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		current, err = tx.GetRoleBotBinding(binding.RoleID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if current != binding {
		return nil, ErrStale
	}
	platform, err := s.resolver.Resolve(ctx, binding)
	if err != nil {
		return nil, err
	}
	if platform == nil || platform.Binding() != binding {
		return nil, fmt.Errorf("resolver returned another bot identity")
	}
	return platform, nil
}

// Run resumes only recorded steps. Independent execution/input work can proceed
// while a document or Tab creation remains unknown; available roles stay usable.
func (s *Service) Run(ctx context.Context, operationID string) (Report, error) {
	var kind string
	var queued work.Operation
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(operationID)
		kind = op.Kind
		queued = op
		return err
	}); err != nil {
		return Report{OperationID: operationID}, err
	}
	if cancellationRequested(queued) {
		return s.settleCancellation(ctx, queued, queued.TargetID)
	}
	if kind == "project.member.provision" {
		return s.runMember(ctx, operationID)
	}
	state, err := s.load(ctx, operationID)
	if err != nil {
		return Report{OperationID: operationID}, err
	}
	report := Report{OperationID: operationID, ProjectID: state.project.ID, Blockers: []string{}}
	if state.project.State == work.ProjectPausing || state.project.State == work.ProjectPaused || state.project.State == work.ProjectArchiving || state.project.State == work.ProjectArchived {
		return s.reportWithError(ctx, state, []string{"项目生命周期暂不允许继续建立"}, fmt.Errorf("project lifecycle suspends provisioning"))
	}
	if state.operation.State == work.OperationConfirmed {
		return s.report(ctx, state, report.Blockers)
	}
	if err := s.applyDirectorySelection(ctx, &state); err != nil {
		return s.reportWithError(ctx, state, []string{"资料目录变更待核查：" + err.Error()}, err)
	}
	stages := []struct {
		id  string
		run func(context.Context, *snapshot) error
	}{{"members", s.members}, {"materials", s.materials}, {"navigation", s.navigation}, {"execution", s.prepareExecution}, {"initial_input", s.initialInput}}
	for _, stage := range stages {
		if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
			if err := s.current(tx, state); err != nil {
				return err
			}
			return s.allowEstablishmentEffect(tx, state)
		}); err != nil {
			return s.reportWithError(ctx, state, append(report.Blockers, err.Error()), err)
		}
		err := stage.run(ctx, &state)
		if err != nil {
			report.Blockers = append(report.Blockers, stage.id+": "+err.Error())
		}
		if e := s.markStage(ctx, &state, stage.id, err); e != nil {
			return report, e
		}
	}
	finished, err := s.finish(ctx, state, report.Blockers)
	if err == nil && state.home.SyncState == "synced" {
		if refreshErr := s.RefreshHome(ctx, state.project.ID); refreshErr != nil {
			finished.Blockers = append(finished.Blockers, "首页索引待同步："+refreshErr.Error())
		}
	}
	return finished, err
}
func (s *Service) report(ctx context.Context, state snapshot, blockers []string) (Report, error) {
	result := Report{OperationID: state.operation.ID, ProjectID: state.project.ID, Blockers: blockers}
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		operation, err := tx.GetOperation(state.operation.ID)
		if err != nil {
			return err
		}
		result.State = string(operation.State)
		result.Steps, err = tx.ListProvisionSteps(operation.ID)
		return err
	})
	return result, err
}
func (s *Service) reportWithError(ctx context.Context, state snapshot, blockers []string, cause error) (Report, error) {
	result, err := s.report(ctx, state, blockers)
	if err != nil {
		return result, err
	}
	return result, cause
}
func roleIDs(state snapshot) []string {
	ids := make([]string, 0, len(state.bindings))
	for id := range state.bindings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
