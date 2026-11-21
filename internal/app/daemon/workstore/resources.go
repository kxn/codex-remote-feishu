package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"strings"
)

func (t *Tx) PutResource(v work.Resource, expected int64) error {
	return t.atomic(func() error {
		if err := v.Validate(); err != nil {
			return err
		}
		scope, err := t.GetScope(v.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeBootstrap {
			return ErrScopeDenied
		}
		if expected > 0 {
			old, err := t.GetResource(v.ScopeID, v.ID)
			if err != nil {
				return err
			}
			if err = immutable(old.ScopeID != v.ScopeID || old.ConnectorIdentity != v.ConnectorIdentity || old.SourceIdentity != v.SourceIdentity); err != nil {
				return err
			}
		}
		if err := t.record("resources", v.ID, v.Revision, expected, v); err != nil {
			return err
		}
		return t.AddResourceCollection(v.ScopeID, v.ID, v.CollectionID)
	})
}
func (t *Tx) GetResource(scopeID, id string) (work.Resource, error) {
	var v work.Resource
	err := t.scopedRead("resources", scopeID, id, &v)
	return v, err
}
func (t *Tx) ListResources(scopeID string) ([]work.Resource, error) {
	return listRecords[work.Resource](t, "resources", " WHERE scope_id=?", scopeID)
}
func (t *Tx) AddResourceCollection(scopeID, resourceID, collectionID string) error {
	if strings.TrimSpace(collectionID) == "" {
		return fmt.Errorf("resource collection required")
	}
	if _, err := t.GetResource(scopeID, resourceID); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, "INSERT INTO resource_collections(resource_id,collection_id) VALUES(?,?) ON CONFLICT DO NOTHING", resourceID, collectionID)
	return err
}
func (t *Tx) PutResourceVersion(v work.ResourceVersion) error {
	return t.atomic(func() error { return t.putResourceVersion(v) })
}
func (t *Tx) putResourceVersion(v work.ResourceVersion) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if _, err := t.GetResource(v.ScopeID, v.ResourceID); err != nil {
		return err
	}
	if v.ContentRef != "" {
		content, err := t.GetContent(v.ScopeID, v.ContentRef)
		if err != nil {
			return err
		}
		if v.ContentHash != "" && strings.TrimPrefix(v.ContentHash, "sha256:") != content.SHA256 {
			return fmt.Errorf("version content hash mismatch")
		}
	}
	old, err := t.GetResourceVersion(v.ScopeID, v.ID)
	if err == nil {
		if !reflect.DeepEqual(old, v) {
			return ErrIdempotencyConflict
		}
		return nil
	}
	if err != ErrNotFound {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO resource_versions(id,payload) VALUES(?,?)", v.ID, string(data))
	if err != nil {
		return err
	}
	return t.invalidateSupersededResourceMemory(v)
}
func (t *Tx) GetResourceVersion(scopeID, id string) (work.ResourceVersion, error) {
	var v work.ResourceVersion
	err := t.scopedRead("resource_versions", scopeID, id, &v)
	return v, err
}
func (t *Tx) ListResourceVersions(scopeID, resourceID string) ([]work.ResourceVersion, error) {
	if _, err := t.GetResource(scopeID, resourceID); err != nil {
		return nil, err
	}
	return listRecords[work.ResourceVersion](t, "resource_versions", " WHERE scope_id=? AND resource_id=?", scopeID, resourceID)
}
func (t *Tx) PutDelivery(v work.Delivery, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	attempt, err := t.GetAttempt(v.SourceAttemptID)
	if err != nil {
		return err
	}
	if attempt.ScopeID != v.ScopeID {
		return ErrScopeDenied
	}
	outbox, err := t.GetOutbox(v.NotificationOutboxID)
	if err != nil {
		return err
	}
	if outbox.ScopeID != v.ScopeID || outbox.Target != v.ConversationTarget {
		return fmt.Errorf("delivery notification scope/target mismatch")
	}
	versions := []work.ResourceVersion{}
	for _, ref := range v.References {
		if _, err = t.GetResource(v.ScopeID, ref.ResourceID); err != nil {
			return err
		}
		if !ref.Current {
			version, e := t.GetResourceVersion(v.ScopeID, ref.VersionID)
			if e != nil {
				return e
			}
			versions = append(versions, version)
		}
	}
	if err = work.ValidateDeliveryVersions(v, versions); err != nil {
		return err
	}
	if expected > 0 {
		old, e := t.GetDelivery(v.ScopeID, v.ID)
		if e != nil {
			return e
		}
		before, after := old, v
		before.Revision = after.Revision
		before.Retention = after.Retention
		if !reflect.DeepEqual(before, after) {
			return fmt.Errorf("delivery manifest is immutable")
		}
		if old.Retention == work.RetentionPermanent && v.Retention != work.RetentionPermanent {
			return fmt.Errorf("permanent delivery retention cannot be downgraded")
		}
	}
	return t.record("deliveries", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetDelivery(scopeID, id string) (work.Delivery, error) {
	var v work.Delivery
	err := t.scopedRead("deliveries", scopeID, id, &v)
	return v, err
}
func (t *Tx) ListDeliveries(scopeID string) ([]work.Delivery, error) {
	return listRecords[work.Delivery](t, "deliveries", " WHERE scope_id=?", scopeID)
}

type PublishDeliveryRequest struct {
	Delivery  work.Delivery
	Operation work.Operation
	Outbox    work.Outbox
}

func (t *Tx) PublishDelivery(request PublishDeliveryRequest) error {
	return t.atomic(func() error {
		v := request.Delivery
		if request.Outbox.ID != v.NotificationOutboxID || request.Outbox.OperationID != request.Operation.ID || request.Outbox.ScopeID != v.ScopeID || request.Operation.ScopeID != v.ScopeID || request.Operation.TargetID != v.ConversationTarget {
			return fmt.Errorf("delivery operation binding mismatch")
		}
		canonical, _, err := t.PutOperation(request.Operation, 0)
		if err != nil {
			return err
		}
		if canonical.ID != request.Outbox.OperationID {
			return ErrIdempotencyConflict
		}
		if err = t.PutOutbox(request.Outbox, 0); err != nil {
			return err
		}
		return t.PutDelivery(v, 0)
	})
}
