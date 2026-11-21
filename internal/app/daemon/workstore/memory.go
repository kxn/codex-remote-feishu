package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
)

func (t *Tx) PutMemory(v work.MemoryEntry, expected int64) error {
	return t.atomic(func() error {
		var previous work.MemoryEntry
		if err := t.validateMemoryOwner(v); err != nil {
			return err
		}
		if v.State == work.MemoryValid {
			if err := t.validateMemoryResourceSources(v); err != nil {
				return err
			}
			if err := t.validateMemoryMessageSources(v); err != nil {
				return err
			}
		}
		if expected > 0 {
			if err := t.read("memories", v.ID, &previous); err != nil {
				return err
			}
			if err := immutable(previous.Scope != v.Scope || previous.StorageScopeID != v.StorageScopeID); err != nil {
				return err
			}
		}
		if err := t.record("memories", v.ID, v.Revision, expected, v); err != nil {
			return err
		}
		if expected > 0 && previous.State == work.MemoryValid && (previous.State != v.State || previous.ContentRef != v.ContentRef || !reflect.DeepEqual(previous.Sources, v.Sources)) {
			return t.notifyMemoryCorrection(previous, v)
		}
		return nil
	})
}

// ReadMemory authorizes semantic ownership first. Only then may it read the
// source body's storage scope, including an explicitly shared team/role entry.
func (t *Tx) ReadMemory(read work.ReadScope, id string) (work.MemoryEntry, work.Content, error) {
	var v work.MemoryEntry
	scope, err := t.readScope(read)
	if err != nil {
		return v, work.Content{}, err
	}
	if err = t.read("memories", id, &v); err != nil {
		return v, work.Content{}, err
	}
	if !memoryAllowed(v.Scope, read, scope) {
		return work.MemoryEntry{}, work.Content{}, ErrScopeDenied
	}
	if err = t.validateMemoryMessageSources(v); err != nil {
		return work.MemoryEntry{}, work.Content{}, ErrNotFound
	}
	if v.State != work.MemoryValid {
		return work.MemoryEntry{}, work.Content{}, ErrNotFound
	}
	if v.Scope.Kind == work.MemoryRole || v.Scope.Kind == work.MemoryTeam {
		if err = t.validateSharing(v.StorageScopeID, v.ContentRef, v.SharingGrantID, memorySharingTarget(v)); err != nil {
			return work.MemoryEntry{}, work.Content{}, err
		}
	}
	content, err := t.GetContent(v.StorageScopeID, v.ContentRef)
	return v, content, err
}
func (t *Tx) ListMemory(read work.ReadScope) ([]work.MemoryEntry, error) {
	scope, err := t.readScope(read)
	if err != nil {
		return nil, err
	}
	all, err := listRecords[work.MemoryEntry](t, "memories", " WHERE state='valid'")
	if err != nil {
		return nil, err
	}
	allowed := []work.MemoryEntry{}
	for _, v := range all {
		if memoryAllowed(v.Scope, read, scope) {
			if t.validateMemoryMessageSources(v) != nil {
				continue
			}
			if (v.Scope.Kind == work.MemoryRole || v.Scope.Kind == work.MemoryTeam) && t.validateSharing(v.StorageScopeID, v.ContentRef, v.SharingGrantID, memorySharingTarget(v)) != nil {
				continue
			}
			allowed = append(allowed, v)
		}
	}
	return allowed, nil
}

// InvalidateMemorySources atomically marks only entries that adopted the exact
// replaced source version; unrelated role/team notes remain usable.
func (t *Tx) InvalidateMemorySources(scopeID string, source work.SourceRef) error {
	return t.atomic(func() error {
		if err := source.Validate(); err != nil {
			return err
		}
		all, err := listRecords[work.MemoryEntry](t, "memories", " WHERE scope_id=? AND state='valid'", scopeID)
		if err != nil {
			return err
		}
		for _, v := range all {
			for _, ref := range v.Sources {
				if reflect.DeepEqual(ref, source) {
					expected := v.Revision
					v.Revision++
					v.State = work.MemoryInvalidated
					if err = t.PutMemory(v, expected); err != nil {
						return err
					}
					break
				}
			}
		}
		return nil
	})
}
