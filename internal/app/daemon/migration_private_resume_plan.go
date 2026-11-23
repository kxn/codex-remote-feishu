package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// sourceResumeIntent selects only the unique, stopped worker and its exact
// original launch. Missing or divergent account/history evidence stays held.
func sourceResumeIntent(source PersonalMigrationSource, seal workstore.PersonalMigrationSeal, report PrivateMigrationReport) *privatechat.MigrationResumeIntent {
	if len(report.History) != 1 || len(report.Checkpoint.QueuedQueueItemIDs) == 0 {
		return nil
	}
	h := report.History[0]
	if h.WorkerID == "" || h.ThreadID == "" || h.Namespace == "" || h.LaunchRecordID == "" || !filepath.IsAbs(h.CWD) || source.Settings.SourceFingerprint == "" {
		return nil
	}
	var stopped personalSourceSealEvidence
	if json.Unmarshal(seal.Evidence, &stopped) != nil {
		return nil
	}
	count := 0
	for _, p := range stopped.Workers {
		if p.WorkerID == h.WorkerID {
			if p.Isolated != nil && !(p.Isolated.ProcessScopesStopped && p.Isolated.NativeCommandsIdle && p.Isolated.InputsIdle && p.Isolated.ToolsIdle && p.Isolated.GoalsIdle && p.Isolated.TasksIdle && p.Isolated.TurnsTerminal && p.Isolated.UncertaintiesEmpty) {
				return nil
			}
			count++
		}
	}
	if count != 1 {
		return nil
	}
	for _, record := range source.Source.Records {
		if record.Table != "content" || record.Key != h.LaunchRecordID {
			continue
		}
		var launch workhost.LaunchRecord
		if json.Unmarshal(record.Data, &launch) != nil {
			return nil
		}
		if launch.WorkerID != h.WorkerID || launch.ScopeID != seal.ScopeID || launch.ConnectionIdentity != h.ConnectionIdentity || launch.Snapshot.CWD != h.CWD || string(launch.Backend) != string(source.Settings.Backend) || launch.ProfileID != source.Settings.ProfileID || launch.SettingsRevision < 1 || launch.SettingsRevision != source.Settings.SettingsRevision {
			return nil
		}
		intent := &privatechat.MigrationResumeIntent{EvidenceVersion: "launch-v1", SourceHash: seal.SourceHash, SourceFingerprint: source.Settings.SourceFingerprint, SourceScope: seal.ScopeID, WorkerID: h.WorkerID, LaunchContentID: h.LaunchRecordID, LaunchContentHash: migrationBytesHash(record.Data), Backend: string(launch.Backend), ProfileID: launch.ProfileID, ConnectionIdentity: h.ConnectionIdentity, ThreadID: h.ThreadID, CWD: h.CWD, Namespace: h.Namespace}
		if launch.NativeHistoryNamespace == h.Namespace && h.ConnectionIdentity != "" {
			return intent
		}
		if launch.Backend != work.BackendCodex || (launch.NativeHistoryNamespace != "" && launch.NativeHistoryNamespace != h.Namespace) {
			return nil
		}
		id := legacyCodexEvidenceID(seal.ScopeID, h.WorkerID, launch.Generation)
		for _, r := range source.Source.Records {
			if r.Table != "content" || r.Key != id {
				continue
			}
			var evidence legacyCodexSessionEvidence
			if json.Unmarshal(r.Data, &evidence) != nil {
				return nil
			}
			snapshotRaw, _ := json.Marshal(launch.Snapshot)
			if evidence.Version != "legacy-codex-v1" || evidence.ScopeID != seal.ScopeID || evidence.WorkerID != h.WorkerID || evidence.WorkerGeneration != launch.Generation || evidence.ThreadID != h.ThreadID || evidence.CWD != h.CWD || evidence.Namespace != h.Namespace || evidence.ProfileID != launch.ProfileID || evidence.SettingsRevision != launch.SettingsRevision || evidence.SourceFingerprint != source.Settings.SourceFingerprint || evidence.LaunchContentID != h.LaunchRecordID || evidence.LaunchSnapshotHash != migrationBytesHash(snapshotRaw) || evidence.JournalEventHash == "" || evidence.SessionMetadataHash == "" || evidence.HistoryRelativePath == "" {
				return nil
			}
			intent.EvidenceVersion = "legacy-codex-v1"
			intent.LegacyEvidenceID = id
			intent.LegacyEvidenceHash = migrationBytesHash(r.Data)
			return intent
		}
		return nil
	}
	return nil
}

// VerifyMigrationResume rechecks the source seal and exact launch against the
// target journal immediately before host startup. It does not call the router.
func (c *personalMigrationCoordinator) VerifyMigrationResume(ctx context.Context, owner privatechat.Owner, generation int64, effectID string, expected privatechat.MigrationResumeIntent) error {
	c.mu.Lock()
	var selected personalMigrationTarget
	matches := 0
	for _, entry := range c.verified {
		if entry.request.Owner == owner && entry.request.Generation == generation {
			selected = entry
			matches++
		}
	}
	c.mu.Unlock()
	if matches != 1 {
		return privatechat.ErrJournalConflict
	}
	plan, err := c.journal.MigrationResumePlan(ctx, owner, generation, effectID)
	if err != nil {
		return err
	}
	if plan != expected {
		return privatechat.ErrJournalConflict
	}
	var seal workstore.PersonalMigrationSeal
	if err = c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		seal, e = tx.GetPersonalMigrationSeal(selected.binding.ScopeID, selected.request.SourceHandle)
		return e
	}); err != nil {
		return err
	}
	if err = c.app.verifyPersonalSourceSealLocal(ctx, seal, c.limits); err != nil {
		return err
	}
	var source PersonalMigrationSource
	if json.Unmarshal(seal.Manifest, &source) != nil {
		return privatechat.ErrJournalConflict
	}
	var worker work.WorkerSession
	found := 0
	for _, r := range source.Source.Records {
		if r.Table == "workers" && r.Key == plan.WorkerID {
			if json.Unmarshal(r.Payload, &worker) != nil {
				return privatechat.ErrJournalConflict
			}
			found++
		}
	}
	if found != 1 || worker.ScopeID != seal.ScopeID || worker.NativeSessionID != plan.ThreadID || worker.NativeNamespace != plan.Namespace {
		return privatechat.ErrJournalConflict
	}
	cp, err := c.journal.LoadCheckpoint(ctx, owner, generation)
	if err != nil {
		return err
	}
	report := PrivateMigrationReport{Checkpoint: cp.Core, History: []PrivateMigrationHistory{{ContextID: worker.ContextID, WorkerID: worker.ID, ThreadID: worker.NativeSessionID, Namespace: worker.NativeNamespace, CWD: plan.CWD, ConnectionIdentity: plan.ConnectionIdentity, LaunchRecordID: plan.LaunchContentID}}}
	actual := sourceResumeIntent(source, seal, report)
	if actual == nil || *actual != plan {
		return privatechat.ErrJournalConflict
	}
	if plan.EvidenceVersion == "legacy-codex-v1" {
		return c.verifyLegacyCodexEvidence(ctx, source, seal, plan)
	}
	return nil
}
