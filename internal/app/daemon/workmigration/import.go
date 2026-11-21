package workmigration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) importRecord(ctx context.Context, item workstore.MigrationItem, override *VerifiedOwner) error {
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		fresh, err := tx.GetMigrationItem(item.MigrationID, item.Key)
		if err != nil {
			return err
		}
		if fresh.Revision != item.Revision {
			return workstore.ErrConflict
		}
		if fresh.State == "imported" {
			return nil
		}
		var record LegacyRecord
		if err = json.Unmarshal(fresh.Payload, &record); err != nil {
			return err
		}
		reason := ""
		state := "imported"
		owner := record.Owner
		if override != nil {
			owner = override
		}
		if record.Kind == "cron" || record.Kind == "config" {
			reason = "legacy_" + record.Kind + "_readonly"
		} else if record.Kind == "preview" {
			state = "quarantined"
			reason = "legacy_preview_requires_verified_durable_copy"
		} else if record.ChatType != "p2p" {
			state = "quarantined"
			reason = "legacy_group_or_chat_type_unproven"
		} else if owner == nil || owner.Identity.Validate() != nil || owner.PrincipalID == "" || owner.RoleID == "" || owner.EvidenceRef == "" {
			state = "quarantined"
			reason = "legacy_identity_requires_verified_sdk_match"
		} else {
			identity := work.ExternalIdentity{PrincipalID: owner.PrincipalID, IdentityScope: owner.Identity}
			err = tx.VerifyMigrationIdentity(identity, owner.EvidenceRef)
			if err == nil {
				_, err = tx.GetRole(owner.RoleID)
			}
			if err != nil {
				if errors.Is(err, workstore.ErrNotFound) || errors.Is(err, workstore.ErrIdentityConflict) || errors.Is(err, workstore.ErrScopeDenied) {
					state = "quarantined"
					reason = "legacy_identity_requires_verified_sdk_match"
				} else {
					return err
				}
			} else {
				unit, e := c.ensureUnit(tx, item.MigrationID, record.UnitKey, *owner)
				if e != nil {
					return e
				}
				fresh.ScopeID, fresh.ContextID = unit.ScopeID, unit.ContextID
				switch record.Kind {
				case "queue":
					if record.Queue == nil || record.Queue.OriginalID == "" || record.Queue.SourceRef == "" || record.Queue.ReceivedAt.IsZero() {
						state = "quarantined"
						reason = "legacy_queue_source_incomplete"
					} else if e = c.importQueue(tx, item.MigrationID, record, unit, *owner); e != nil {
						return e
					}
				case "native":
					if record.Native == nil || record.Native.Namespace == "" || record.Native.SessionID == "" || (record.Native.Backend != work.BackendCodex && record.Native.Backend != work.BackendClaude && record.Native.Backend != work.BackendOpenCode) {
						state = "quarantined"
						reason = "legacy_native_identity_incomplete"
					} else if e = c.importNative(tx, item.MigrationID, record, unit); e != nil {
						if errors.Is(e, workstore.ErrIdentityConflict) {
							state = "quarantined"
							reason = "legacy_native_has_multiple_possible_owners"
						} else {
							return e
						}
					}
				}
				raw, _ := json.Marshal(owner)
				fresh.Owner = string(raw)
			}
		}
		fresh.State, fresh.Reason = state, reason
		fresh.Revision++
		return tx.UpdateMigrationItem(fresh, item.Revision)
	})
}
func (c *Coordinator) ensureUnit(tx *workstore.Tx, id, key string, owner VerifiedOwner) (workstore.MigrationUnit, error) {
	old, err := tx.GetMigrationUnit(id, key)
	if err == nil {
		if old.PrincipalID != owner.PrincipalID || old.RoleID != owner.RoleID {
			return old, workstore.ErrIdentityConflict
		}
		return old, nil
	}
	if !isMissing(err) {
		return old, err
	}
	if _, err = tx.GetPrincipal(owner.PrincipalID); err != nil {
		return old, err
	}
	if _, err = tx.GetRole(owner.RoleID); err != nil {
		return old, err
	}
	scope, err := tx.FindPersonalScope(owner.PrincipalID, owner.RoleID)
	if isMissing(err) {
		scope = work.WorkScope{ID: stableID("personal", owner.PrincipalID, owner.RoleID), Kind: work.ScopePersonal, PrincipalID: owner.PrincipalID, RoleID: owner.RoleID, Revision: 1, CreatedAt: c.options.Clock()}
		err = tx.PutScope(scope, 0)
	}
	if err != nil {
		return old, err
	}
	context := work.WorkContext{ID: stableID("legacy_context", id, key), ScopeID: scope.ID, RoleID: owner.RoleID, Workstream: "legacy:" + key, Revision: 1, CreatedAt: c.options.Clock(), UpdatedAt: c.options.Clock()}
	if existing, e := tx.GetContext(context.ID); e == nil {
		context = existing
	} else if !isMissing(e) {
		return old, e
	} else if e = tx.PutContext(context, 0); e != nil {
		return old, e
	}
	unit := workstore.MigrationUnit{MigrationID: id, UnitKey: key, PrincipalID: owner.PrincipalID, RoleID: owner.RoleID, ScopeID: scope.ID, ContextID: context.ID}
	err = tx.PutMigrationUnit(unit)
	return unit, err
}
func (c *Coordinator) importQueue(tx *workstore.Tx, id string, record LegacyRecord, unit workstore.MigrationUnit, owner VerifiedOwner) error {
	queue := record.Queue
	inputKey := stableID("queue", id, record.UnitKey, queue.SourceRef, queue.OriginalID)
	raw, _ := json.Marshal(record)
	source := work.NewContent(stableID("legacy_source", id, record.Key), unit.ScopeID, "application/json", raw, c.options.Clock())
	if err := tx.PutContent(source); err != nil {
		return err
	}
	body := work.NewContent(stableID("legacy_input", inputKey), unit.ScopeID, "text/plain", []byte(queue.Text), c.options.Clock())
	if err := tx.PutContent(body); err != nil {
		return err
	}
	message := work.Message{ID: stableID("legacy_message", inputKey), ScopeID: unit.ScopeID, ConversationID: stableID("legacy_conversation", id, record.UnitKey), Source: work.Actor{Kind: work.ActorPrincipal, ID: owner.PrincipalID}, BodyRef: body.ID, DerivedFrom: &work.SourceRef{Kind: "legacy_queue", ID: queue.OriginalID, Version: id, EvidenceRef: source.ID}, Revision: 1, CreatedAt: queue.ReceivedAt}
	inbox := work.Inbox{ID: stableID("legacy_inbox", inputKey), ScopeID: unit.ScopeID, RecipientContextID: unit.ContextID, RecipientRoleID: unit.RoleID, EventID: stableID("legacy_event", id, queue.OriginalID), MessageID: message.ID, State: work.InboxPending, AvailableAt: queue.ReceivedAt, Revision: 1}
	if existing, e := tx.GetInbox(inbox.ID); e == nil {
		old, e := tx.GetMessage(existing.MessageID)
		if e != nil {
			return e
		}
		if old.Source != message.Source || old.BodyRef != message.BodyRef || existing.RecipientContextID != unit.ContextID {
			return workstore.ErrIdempotencyConflict
		}
		return nil
	} else if !isMissing(e) {
		return e
	}
	_, _, err := tx.AcceptMessage(message, []work.Inbox{inbox})
	return err
}
func (c *Coordinator) importNative(tx *workstore.Tx, id string, record LegacyRecord, unit workstore.MigrationUnit) error {
	native := record.Native
	if native.Backend != work.BackendCodex && native.Backend != work.BackendClaude && native.Backend != work.BackendOpenCode {
		return fmt.Errorf("unknown legacy native backend")
	}
	// Shared native history is never assigned on the basis of one surface's
	// last actor. All references must first resolve to the same precise owner.
	var snapshot Snapshot
	state, err := tx.GetMigration(id)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(state.Snapshot, &snapshot); err != nil {
		return err
	}
	for _, other := range snapshot.Records {
		if other.Native == nil || other.UnitKey == record.UnitKey || nativeResourceKey(*other.Native) != nativeResourceKey(*native) {
			continue
		}
		otherUnit, e := tx.GetMigrationUnit(id, other.UnitKey)
		if e != nil || otherUnit.PrincipalID != unit.PrincipalID || otherUnit.RoleID != unit.RoleID {
			return workstore.ErrIdentityConflict
		}
	}
	workerID := stableID("worker", unit.ContextID)
	if err = tx.AssertMigrationNativeOwner(nativeResourceKey(*native), unit, workerID); err != nil {
		return err
	}
	worker := work.WorkerSession{ID: workerID, ScopeID: unit.ScopeID, ContextID: unit.ContextID, RoleID: unit.RoleID, Backend: native.Backend, State: work.WorkerRecovering, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: native.InstanceID, NativeNamespace: native.Namespace, NativeSessionID: native.SessionID, Revision: 1, UpdatedAt: c.options.Clock()}
	if existing, e := tx.GetWorker(workerID); e == nil {
		if existing.NativeSessionID != native.SessionID || existing.NativeNamespace != native.Namespace || existing.Backend != native.Backend {
			return workstore.ErrIdentityConflict
		}
	} else if !isMissing(e) {
		return e
	} else if e = tx.PutWorker(worker, 0); e != nil {
		return e
	}
	if err = tx.RegisterMigrationNativeOwner(nativeResourceKey(*native), unit, workerID); err != nil {
		return err
	}
	reason := "legacy_native_requires_verified_quiescence_and_fresh_rebind"
	if native.CWD != "" {
		reason += "_location_preserved_readonly"
	}
	if strings.TrimSpace(native.InstanceID) == "" {
		reason += "_instance_unknown"
	}
	return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: workerID, ScopeID: unit.ScopeID, Reason: reason, ResponsibleRoleID: unit.RoleID, CreatedAt: c.options.Clock()})
}
