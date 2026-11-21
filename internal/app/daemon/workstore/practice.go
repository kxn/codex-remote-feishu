package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
)

func practiceKey(owner work.KnowledgeOwner, ref work.PracticeRef) string {
	return mustJSON(struct {
		Owner work.KnowledgeOwner
		Ref   work.PracticeRef
	}{owner, ref})
}
func (t *Tx) PutPractice(v work.PracticeDefinition, expected int64) error {
	return t.putPractice(v, expected, false)
}
func (t *Tx) putPractice(v work.PracticeDefinition, expected int64, trusted bool) error {
	if !trusted {
		if err := t.validateSharing(v.StorageScopeID, v.ContentRef, v.SharingGrantID, practiceSharingTarget(v)); err != nil {
			return err
		}
	}
	if err := v.Validate(); err != nil {
		return err
	}
	if err := t.requireContent(v.StorageScopeID, v.ContentRef); err != nil {
		return err
	}
	scope, err := t.GetScope(v.StorageScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeBootstrap {
		return ErrScopeDenied
	}
	if v.Owner.Kind == work.KnowledgeTeam && (scope.Kind != work.ScopeProject || v.Owner.TeamID != scope.TeamID) {
		return ErrScopeDenied
	}
	if v.Owner.Kind == work.KnowledgeRole {
		if err = t.exists("roles", v.Owner.RoleID); err != nil {
			return err
		}
		if scope.Kind == work.ScopePersonal && scope.RoleID != v.Owner.RoleID {
			return ErrScopeDenied
		}
	}
	key := practiceKey(v.Owner, v.PracticeRef)
	if expected > 0 {
		var old work.PracticeDefinition
		if err = t.read("practices", key, &old); err != nil {
			return err
		}
		old.Revision = v.Revision
		if !reflect.DeepEqual(old, v) {
			return fmt.Errorf("published practice versions are immutable; publish a new version")
		}
	}
	return t.record("practices", key, v.Revision, expected, v)
}
func (t *Tx) GetPracticeFor(read work.ReadScope, owner work.KnowledgeOwner, ref work.PracticeRef) (work.PracticeDefinition, work.Content, error) {
	var v work.PracticeDefinition
	if err := t.authorizeKnowledge(read, owner); err != nil {
		return v, work.Content{}, err
	}
	if err := ref.Validate(); err != nil {
		return v, work.Content{}, err
	}
	if err := t.read("practices", practiceKey(owner, ref), &v); err != nil {
		return v, work.Content{}, err
	}
	if err := t.practiceReadable(v); err != nil {
		return work.PracticeDefinition{}, work.Content{}, err
	}
	content, err := t.GetContent(v.StorageScopeID, v.ContentRef)
	return v, content, err
}
func (t *Tx) ListPracticesFor(read work.ReadScope) ([]work.PracticeDefinition, error) {
	if _, err := t.readScope(read); err != nil {
		return nil, err
	}
	all, err := listRecords[work.PracticeDefinition](t, "practices", "")
	if err != nil {
		return nil, err
	}
	allowed := []work.PracticeDefinition{}
	for _, v := range all {
		if t.authorizeKnowledge(read, v.Owner) == nil && t.practiceReadable(v) == nil {
			allowed = append(allowed, v)
		}
	}
	return allowed, nil
}
func (t *Tx) PutPracticeAdoption(v work.PracticeAdoption, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	var definition work.PracticeDefinition
	if err := t.read("practices", practiceKey(v.Owner, v.Practice), &definition); err != nil {
		return err
	}
	if err := t.practiceReadable(definition); err != nil {
		return err
	}
	scope, err := t.GetScope(v.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeBootstrap {
		return ErrScopeDenied
	}
	if v.Owner.Kind == work.KnowledgeTeam && (scope.Kind != work.ScopeProject || scope.TeamID != v.Owner.TeamID) {
		return ErrScopeDenied
	}
	if v.Owner.Kind == work.KnowledgeRole && v.AdoptedBy.Kind == work.ActorRole && v.AdoptedBy.ID != v.Owner.RoleID {
		return ErrScopeDenied
	}
	if v.Owner.Kind == work.KnowledgeRole {
		if err = t.requireRoleInScope(v.ScopeID, v.Owner.RoleID); err != nil {
			return err
		}
	}
	if v.AdoptedBy.Kind == work.ActorRole {
		if err = t.requireRoleInScope(v.ScopeID, v.AdoptedBy.ID); err != nil {
			return err
		}
	}
	if v.AdoptedBy.Kind == work.ActorPrincipal && scope.Kind == work.ScopePersonal && v.AdoptedBy.ID != scope.PrincipalID {
		return ErrScopeDenied
	}
	if v.TaskID != "" {
		task, e := t.GetTask(v.TaskID)
		if e != nil {
			return e
		}
		if task.ScopeID != v.ScopeID {
			return ErrScopeDenied
		}
	}
	if expected > 0 {
		old, e := t.GetPracticeAdoption(v.ScopeID, v.ID)
		if e != nil {
			return e
		}
		if e = immutable(old.ScopeID != v.ScopeID || old.TaskID != v.TaskID || old.Practice != v.Practice || old.Owner != v.Owner); e != nil {
			return e
		}
	}
	return t.record("practice_adoptions", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetPracticeAdoption(scopeID, id string) (work.PracticeAdoption, error) {
	var v work.PracticeAdoption
	err := t.scopedRead("practice_adoptions", scopeID, id, &v)
	return v, err
}
func (t *Tx) ListPracticeAdoptions(scopeID string) ([]work.PracticeAdoption, error) {
	return listRecords[work.PracticeAdoption](t, "practice_adoptions", " WHERE scope_id=?", scopeID)
}
