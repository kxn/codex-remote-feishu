package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) readScope(read work.ReadScope) (work.WorkScope, error) {
	if err := read.Validate(); err != nil {
		return work.WorkScope{}, err
	}
	scope, err := t.GetScope(read.ScopeID)
	if err != nil {
		return scope, err
	}
	if scope.Kind == work.ScopeBootstrap {
		return scope, ErrScopeDenied
	}
	if scope.Kind == work.ScopePersonal {
		if scope.RoleID != read.RoleID {
			return scope, ErrScopeDenied
		}
	} else {
		if err = t.requireProjectReady(scope.ProjectID, read.RoleID); err != nil {
			return scope, err
		}
		member, e := t.GetTeamMember(scope.TeamID, read.RoleID)
		if e != nil || !member.Active {
			return scope, ErrScopeDenied
		}
	}
	if read.ContextID != "" {
		context, e := t.GetContext(read.ContextID)
		if e != nil {
			return scope, e
		}
		if context.ScopeID != scope.ID || context.RoleID != read.RoleID {
			return scope, ErrScopeDenied
		}
	}
	if read.TaskID != "" {
		task, e := t.GetTask(read.TaskID)
		if e != nil {
			return scope, e
		}
		if task.ScopeID != scope.ID {
			return scope, ErrScopeDenied
		}
	}
	return scope, nil
}
func memoryAllowed(owner work.MemoryScope, read work.ReadScope, scope work.WorkScope) bool {
	switch owner.Kind {
	case work.MemoryRole:
		return owner.RoleID == read.RoleID
	case work.MemoryTeam:
		return scope.Kind == work.ScopeProject && owner.TeamID == scope.TeamID
	case work.MemoryProject:
		return scope.Kind == work.ScopeProject && owner.ProjectID == scope.ProjectID
	case work.MemoryRoleProject:
		return scope.Kind == work.ScopeProject && owner.ProjectID == scope.ProjectID && owner.RoleID == read.RoleID
	case work.MemoryPersonal:
		return scope.Kind == work.ScopePersonal && owner.PrincipalID == scope.PrincipalID && owner.RoleID == read.RoleID
	case work.MemoryTask:
		return read.TaskID != "" && owner.TaskID == read.TaskID
	case work.MemoryContext:
		return read.ContextID != "" && owner.ContextID == read.ContextID
	}
	return false
}
func (t *Tx) authorizeKnowledge(read work.ReadScope, owner work.KnowledgeOwner) error {
	if err := owner.Validate(); err != nil {
		return err
	}
	scope, err := t.readScope(read)
	if err != nil {
		return err
	}
	switch owner.Kind {
	case work.KnowledgeGlobal:
		return nil
	case work.KnowledgeTeam:
		if scope.Kind == work.ScopeProject && scope.TeamID == owner.TeamID {
			return nil
		}
	case work.KnowledgeRole:
		if read.RoleID == owner.RoleID {
			return nil
		}
	}
	return ErrScopeDenied
}
func (t *Tx) validateMemoryOwner(v work.MemoryEntry) error {
	if err := v.Validate(); err != nil {
		return err
	}
	scope, err := t.GetScope(v.StorageScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeBootstrap {
		return ErrScopeDenied
	}
	// Storage provenance cannot be used to promote a private context or another
	// project's source to broader ownership without an explicit sharing use case.
	switch v.Scope.Kind {
	case work.MemoryRole:
		if err = t.requireRoleInScope(v.StorageScopeID, v.Scope.RoleID); err != nil {
			return err
		}
		if err = t.exists("roles", v.Scope.RoleID); err != nil {
			return err
		}
		if scope.Kind == work.ScopePersonal && scope.RoleID != v.Scope.RoleID {
			return ErrScopeDenied
		}
	case work.MemoryTeam:
		if scope.Kind != work.ScopeProject || scope.TeamID != v.Scope.TeamID {
			return ErrScopeDenied
		}
	case work.MemoryProject, work.MemoryRoleProject:
		if scope.Kind != work.ScopeProject || scope.ProjectID != v.Scope.ProjectID {
			return ErrScopeDenied
		}
		if v.Scope.Kind == work.MemoryRoleProject {
			member, e := t.GetTeamMember(scope.TeamID, v.Scope.RoleID)
			if e != nil || !member.Active {
				return ErrScopeDenied
			}
		}
	case work.MemoryPersonal:
		if scope.Kind != work.ScopePersonal || scope.PrincipalID != v.Scope.PrincipalID || scope.RoleID != v.Scope.RoleID {
			return ErrScopeDenied
		}
	case work.MemoryTask:
		task, e := t.GetTask(v.Scope.TaskID)
		if e != nil {
			return e
		}
		if task.ScopeID != scope.ID {
			return ErrScopeDenied
		}
	case work.MemoryContext:
		context, e := t.GetContext(v.Scope.ContextID)
		if e != nil {
			return e
		}
		if context.ScopeID != scope.ID {
			return ErrScopeDenied
		}
	default:
		return fmt.Errorf("invalid memory ownership")
	}
	if (v.Scope.Kind == work.MemoryRole || v.Scope.Kind == work.MemoryTeam) && v.State == work.MemoryValid {
		if err = t.validateSharing(v.StorageScopeID, v.ContentRef, v.SharingGrantID, memorySharingTarget(v)); err != nil {
			return err
		}
	}
	return t.requireContent(v.StorageScopeID, v.ContentRef)
}
