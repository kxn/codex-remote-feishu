package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"strconv"
)

func (t *Tx) PutSharingGrant(v work.SharingGrant, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	content, err := t.GetContent(v.SourceScopeID, v.ContentRef)
	if err != nil {
		return err
	}
	if content.SHA256 != v.ContentSHA256 {
		return fmt.Errorf("sharing source content changed")
	}
	attempt, err := t.GetAttemptByRunRef(v.AuthorizingRunRef.RunID)
	if err != nil {
		return err
	}
	if attempt.RunRef != v.AuthorizingRunRef || attempt.ScopeID != v.SourceScopeID || attempt.State != work.AttemptRunning {
		return fmt.Errorf("sharing requires current admitted human instruction")
	}
	worker, err := t.GetWorker(v.AuthorizingRunRef.WorkerID)
	if err != nil {
		return err
	}
	if err = work.ValidateAttemptBinding(attempt, worker); err != nil {
		return err
	}
	admitted := false
	for _, id := range attempt.InputIDs {
		if id == v.SourceInboxID {
			admitted = true
		}
	}
	if !admitted {
		return fmt.Errorf("sharing message not admitted to authorizing attempt")
	}
	input, err := t.GetInbox(v.SourceInboxID)
	if err != nil {
		return err
	}
	if input.State != work.InboxClaimed || input.ClaimedAttemptID != attempt.ID || input.MessageID != v.SourceMessageID || input.ScopeID != attempt.ScopeID {
		return fmt.Errorf("sharing input claim mismatch")
	}
	message, err := t.GetMessage(v.SourceMessageID)
	if err != nil {
		return err
	}
	if message.Source != v.AuthorizedBy || message.ScopeID != v.SourceScopeID {
		return fmt.Errorf("sharing source is not the authorized human")
	}
	scope, err := t.GetScope(v.SourceScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeBootstrap {
		return ErrScopeDenied
	}
	if scope.Kind == work.ScopePersonal && scope.PrincipalID != v.AuthorizedBy.ID {
		return ErrScopeDenied
	}
	if expected > 0 {
		old, e := t.GetSharingGrant(v.SourceScopeID, v.ID)
		if e != nil {
			return e
		}
		if e = immutable(old.SourceScopeID != v.SourceScopeID || old.ContentRef != v.ContentRef || old.ContentSHA256 != v.ContentSHA256 || !reflect.DeepEqual(old.Target, v.Target) || old.AuthorizedBy != v.AuthorizedBy); e != nil {
			return e
		}
	}
	return t.record("sharing_grants", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetSharingGrant(scopeID, id string) (work.SharingGrant, error) {
	var v work.SharingGrant
	err := t.scopedRead("sharing_grants", scopeID, id, &v)
	return v, err
}
func (t *Tx) validateSharing(scopeID, ref, grantID string, target work.SharingTarget) error {
	if grantID == "" {
		return fmt.Errorf("shared knowledge requires explicit scoped sharing grant")
	}
	grant, err := t.GetSharingGrant(scopeID, grantID)
	if err != nil {
		return err
	}
	if grant.State != work.SharingActive || grant.ContentRef != ref || !reflect.DeepEqual(grant.Target, target) {
		return fmt.Errorf("sharing grant revoked or target/version mismatch")
	}
	content, err := t.GetContent(scopeID, ref)
	if err != nil {
		return err
	}
	if grant.ContentSHA256 != content.SHA256 {
		return fmt.Errorf("sharing content digest mismatch")
	}
	return nil
}
func memorySharingTarget(v work.MemoryEntry) work.SharingTarget {
	owner := v.Scope
	return work.SharingTarget{Kind: work.SharingMemory, ObjectID: v.ID, ObjectVersion: strconv.FormatInt(v.Revision, 10), MemoryScope: &owner}
}
func practiceSharingTarget(v work.PracticeDefinition) work.SharingTarget {
	owner := v.Owner
	return work.SharingTarget{Kind: work.SharingPractice, ObjectID: v.ID, ObjectVersion: v.Version, KnowledgeOwner: &owner}
}
