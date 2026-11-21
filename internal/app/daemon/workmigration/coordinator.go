package workmigration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Coordinator struct {
	store   *workstore.Store
	host    Host
	options Options
}

func New(store *workstore.Store, host Host, options Options) (*Coordinator, error) {
	if store == nil || host == nil || !filepath.IsAbs(options.StateDir) {
		return nil, fmt.Errorf("migration requires durable store, startup host fencer and absolute state directory")
	}
	if options.Clock == nil {
		options.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &Coordinator{store: store, host: host, options: options}, nil
}
func (c *Coordinator) Prepare(ctx context.Context, snapshot Snapshot) (Status, error) {
	fingerprint, err := Fingerprint(snapshot)
	if err != nil {
		return Status{}, err
	}
	if snapshot.SourceFingerprint == "" {
		snapshot.SourceFingerprint = fingerprint
	} else if snapshot.SourceFingerprint != fingerprint {
		return Status{}, fmt.Errorf("migration source fingerprint mismatch")
	}
	for _, backup := range snapshot.Backups {
		if err = c.verifyBackup(backup); err != nil {
			return Status{}, err
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return Status{}, err
	}
	if len(raw) > 64<<20 {
		return Status{}, fmt.Errorf("migration capture exceeds bounded snapshot limit")
	}
	items := make([]workstore.MigrationItem, 0, len(snapshot.Records))
	for _, record := range snapshot.Records {
		payload, e := json.Marshal(record)
		if e != nil {
			return Status{}, e
		}
		items = append(items, workstore.MigrationItem{MigrationID: snapshot.ID, Key: record.Key, UnitKey: record.UnitKey, Kind: record.Kind, Payload: payload})
	}
	err = c.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.PrepareMigration(workstore.MigrationState{ID: snapshot.ID, SourceFingerprint: snapshot.SourceFingerprint, Phase: "captured", Revision: 1, Snapshot: raw}, items)
	})
	if err != nil {
		return Status{}, err
	}
	return c.Status(ctx, snapshot.ID)
}
func (c *Coordinator) load(ctx context.Context, id string) (workstore.MigrationState, Snapshot, error) {
	var state workstore.MigrationState
	var snapshot Snapshot
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		state, e = tx.GetMigration(id)
		if e != nil {
			return e
		}
		return json.Unmarshal(state.Snapshot, &snapshot)
	})
	return state, snapshot, err
}
func (c *Coordinator) Fence(ctx context.Context, id string) (Status, error) {
	state, snapshot, err := c.load(ctx, id)
	if err != nil {
		return Status{}, err
	}
	if state.Phase != "captured" {
		return c.Status(ctx, id)
	}
	receipt, err := c.host.Fence(ctx, snapshot)
	if err != nil {
		return Status{}, err
	}
	if err = validateFence(snapshot, receipt); err != nil {
		return Status{}, err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return Status{}, err
	}
	err = c.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetMigration(id)
		if e != nil {
			return e
		}
		if current.Revision != state.Revision {
			return workstore.ErrConflict
		}
		for _, native := range receipt.Native {
			if native.Classification == "stopped" {
				continue
			}
			record := findRecord(snapshot, native.RecordKey)
			evidence, _ := json.Marshal(map[string]any{"native": record.Native, "classification": native.Classification, "observation": json.RawMessage(native.Evidence)})
			if e = putNativeFences(tx, id, record.Key, *record.Native, "legacy_native_"+native.Classification, evidence); e != nil {
				return e
			}
		}
		current.Phase = "quiescing"
		current.Fence = raw
		current.Revision++
		return tx.UpdateMigration(current, state.Revision)
	})
	if err != nil {
		return Status{}, err
	}
	return c.Status(ctx, id)
}
func (c *Coordinator) Import(ctx context.Context, id string, limit int) (Status, error) {
	state, _, err := c.load(ctx, id)
	if err != nil {
		return Status{}, err
	}
	if state.Phase == "imported" || state.Phase == "authority_committed" || state.Phase == "finalized" {
		return c.Status(ctx, id)
	}
	if state.Phase != "quiescing" {
		return Status{}, fmt.Errorf("legacy writers and in-flight resources must be fenced before import")
	}
	var items []workstore.MigrationItem
	err = c.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		items, e = tx.ListMigrationItems(id, "pending", limit)
		return e
	})
	if err != nil {
		return Status{}, err
	}
	for _, item := range items {
		if err = c.importRecord(ctx, item, nil); err != nil {
			return Status{}, err
		}
	}
	err = c.store.Within(ctx, func(tx *workstore.Tx) error {
		counts, e := tx.MigrationCounts(id)
		if e != nil {
			return e
		}
		if counts["pending"] != 0 {
			return nil
		}
		current, e := tx.GetMigration(id)
		if e != nil {
			return e
		}
		if current.Phase != "quiescing" {
			return nil
		}
		before := current.Revision
		current.Revision++
		current.Phase = "imported"
		return tx.UpdateMigration(current, before)
	})
	if err != nil {
		return Status{}, err
	}
	return c.Status(ctx, id)
}
func (c *Coordinator) Commit(ctx context.Context, id string) (Status, error) {
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		state, e := tx.GetMigration(id)
		if e != nil {
			return e
		}
		return tx.CommitMigrationAuthority(id, state.AuthorityGeneration)
	})
	if err != nil {
		return Status{}, err
	}
	return c.Status(ctx, id)
}
func (c *Coordinator) Finalize(ctx context.Context, id string) (Status, error) {
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		state, e := tx.GetMigration(id)
		if e != nil {
			return e
		}
		if state.Phase == "finalized" {
			return nil
		}
		if state.Phase != "authority_committed" {
			return fmt.Errorf("authority has not committed")
		}
		before := state.Revision
		state.Revision++
		state.Phase = "finalized"
		return tx.UpdateMigration(state, before)
	})
	if err != nil {
		return Status{}, err
	}
	return c.Status(ctx, id)
}
func (c *Coordinator) Status(ctx context.Context, id string) (Status, error) {
	var status Status
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		state, e := tx.GetMigration(id)
		if e != nil {
			return e
		}
		counts, e := tx.MigrationCounts(id)
		if e != nil {
			return e
		}
		authority, e := tx.GetMigrationAuthority()
		if e != nil {
			return e
		}
		status = Status{ID: id, SourceFingerprint: state.SourceFingerprint, Phase: state.Phase, Revision: state.Revision, AuthorityGeneration: authority.Generation, Pending: counts["pending"], Imported: counts["imported"], Quarantined: counts["quarantined"]}
		return nil
	})
	return status, err
}
func (c *Coordinator) Adopt(ctx context.Context, id, key string, owner VerifiedOwner) (Status, error) {
	var item workstore.MigrationItem
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		state, e := tx.GetMigration(id)
		if e != nil {
			return e
		}
		if state.Phase != "imported" && state.Phase != "authority_committed" && state.Phase != "finalized" {
			return fmt.Errorf("migration capture must finish before identity adoption")
		}
		item, e = tx.GetMigrationItem(id, key)
		return e
	})
	if err != nil {
		return Status{}, err
	}
	if item.State == "imported" {
		var previous VerifiedOwner
		if json.Unmarshal([]byte(item.Owner), &previous) != nil || previous.Identity != owner.Identity || previous.PrincipalID != owner.PrincipalID || previous.RoleID != owner.RoleID {
			return Status{}, workstore.ErrIdentityConflict
		}
		if err = c.store.Within(ctx, func(tx *workstore.Tx) error {
			return tx.VerifyMigrationIdentity(work.ExternalIdentity{PrincipalID: owner.PrincipalID, IdentityScope: owner.Identity}, owner.EvidenceRef)
		}); err != nil {
			return Status{}, err
		}
		return c.Status(ctx, id)
	}
	if err = c.importRecord(ctx, item, &owner); err != nil {
		return Status{}, err
	}
	return c.Status(ctx, id)
}
func (c *Coordinator) ObserveLegacy(ctx context.Context, instanceID, kind string, payload json.RawMessage) error {
	if len(payload) > 4<<20 {
		return fmt.Errorf("legacy observation exceeds bounded payload limit")
	}
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ObserveLegacyRuntime(instanceID, kind, payload, c.options.Clock()); err != nil {
			return err
		}
		return c.fenceLegacyEvents(tx, instanceID, kind, payload)
	})
}

func validateFence(snapshot Snapshot, receipt FenceReceipt) error {
	if receipt.MigrationID != snapshot.ID || receipt.SourceFingerprint != snapshot.SourceFingerprint || !receipt.LegacyWritersClosed || !receipt.QueuesCaptured || !receipt.IngressClosed || !receipt.CronDisabled || !receipt.InflightReconciled || !validEvidence(receipt.Evidence) {
		return fmt.Errorf("startup fencing evidence is incomplete or for another source")
	}
	seen := map[string]bool{}
	for _, native := range receipt.Native {
		record := findRecord(snapshot, native.RecordKey)
		if record == nil || record.Native == nil || seen[native.RecordKey] || !validEvidence(native.Evidence) || (native.Classification != "stopped" && native.Classification != "transferred" && native.Classification != "unknown") {
			return fmt.Errorf("in-flight native lacks an exact classification")
		}
		seen[native.RecordKey] = true
	}
	for _, record := range snapshot.Records {
		if record.Native != nil && !seen[record.Key] {
			return fmt.Errorf("a captured native remains unclassified")
		}
	}
	return nil
}
func findRecord(snapshot Snapshot, key string) *LegacyRecord {
	for i := range snapshot.Records {
		if snapshot.Records[i].Key == key {
			return &snapshot.Records[i]
		}
	}
	return nil
}
func validEvidence(raw json.RawMessage) bool {
	var v map[string]json.RawMessage
	return json.Unmarshal(raw, &v) == nil && len(v) > 0
}
func isMissing(err error) bool { return errors.Is(err, workstore.ErrNotFound) }
