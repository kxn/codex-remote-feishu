package daemon

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestSourceResumeIntentRequiresStoppedExactLaunch(t *testing.T) {
	launch := workhost.LaunchRecord{WorkerID: "worker", ScopeID: "scope", Generation: 1, Backend: work.BackendClaude, ProfileID: "profile", SettingsRevision: 7, NativeHistoryNamespace: "namespace", ConnectionIdentity: "account", Snapshot: agentproto.RuntimeLaunchSnapshot{CWD: "/work"}}
	raw, _ := json.Marshal(launch)
	source := PersonalMigrationSource{Settings: PersonalMigrationSettings{Backend: agentproto.BackendClaude, ProfileID: "profile", SettingsRevision: 7, SourceFingerprint: migrationBytesHash([]byte("settings"))}, Source: workstore.PersonalExport{Records: []workstore.PersonalExportRecord{{Table: "content", Key: "launch", Data: raw}}}}
	report := PrivateMigrationReport{History: []PrivateMigrationHistory{{WorkerID: "worker", ThreadID: "thread", CWD: "/work", Namespace: "namespace", ConnectionIdentity: "account", LaunchRecordID: "launch"}}, Checkpoint: orchestrator.PrivateCheckpoint{QueuedQueueItemIDs: []string{"queue-1"}}}
	stopped, _ := json.Marshal(personalSourceSealEvidence{Workers: []personalSourceStopProof{{WorkerID: "worker"}}})
	seal := workstore.PersonalMigrationSeal{ScopeID: "scope", SourceHash: migrationBytesHash([]byte("manifest")), Evidence: stopped}
	intent := sourceResumeIntent(source, seal, report)
	if intent == nil || intent.ThreadID != "thread" || intent.Namespace != "namespace" || intent.ConnectionIdentity != "account" {
		t.Fatal("exact stopped launch not retained")
	}
	bad := source
	bad.Settings.ProfileID = "other"
	if sourceResumeIntent(bad, seal, report) != nil {
		t.Fatal("changed profile accepted")
	}
	bad = source
	bad.Settings.SettingsRevision++
	if sourceResumeIntent(bad, seal, report) != nil {
		t.Fatal("changed settings revision accepted")
	}
	report.History[0].Namespace = "different"
	if sourceResumeIntent(source, seal, report) != nil {
		t.Fatal("different history namespace accepted")
	}
	report.History[0].Namespace = "namespace"
	seal.Evidence = []byte(`{"workers":[]}`)
	if sourceResumeIntent(source, seal, report) != nil {
		t.Fatal("missing stop evidence accepted")
	}
}

func TestSourceResumeIntentAcceptsVersionedCodexProvenanceWithoutInventingAccount(t *testing.T) {
	launch := workhost.LaunchRecord{WorkerID: "worker", ScopeID: "scope", Generation: 1, Backend: work.BackendCodex, ProfileID: "profile", SettingsRevision: 7, Snapshot: agentproto.RuntimeLaunchSnapshot{CWD: "/work"}}
	launchRaw, _ := json.Marshal(launch)
	snapshotRaw, _ := json.Marshal(launch.Snapshot)
	fingerprint := migrationBytesHash([]byte("settings"))
	evidence := legacyCodexSessionEvidence{Version: "legacy-codex-v1", ScopeID: "scope", WorkerID: "worker", WorkerGeneration: 1, ThreadID: "thread", CWD: "/work", Namespace: "namespace", ProfileID: "profile", SettingsRevision: 7, SourceFingerprint: fingerprint, LaunchContentID: "launch", LaunchSnapshotHash: migrationBytesHash(snapshotRaw), JournalEventHash: "event-hash", HistoryRelativePath: "2026/session.jsonl", SessionMetadataHash: "metadata-hash"}
	evidenceRaw, _ := json.Marshal(evidence)
	id := legacyCodexEvidenceID("scope", "worker", 1)
	source := PersonalMigrationSource{Settings: PersonalMigrationSettings{Backend: agentproto.BackendCodex, ProfileID: "profile", SettingsRevision: 7, SourceFingerprint: fingerprint}, Source: workstore.PersonalExport{Records: []workstore.PersonalExportRecord{{Table: "content", Key: "launch", Data: launchRaw}, {Table: "content", Key: id, Data: evidenceRaw}}}}
	report := PrivateMigrationReport{History: []PrivateMigrationHistory{{WorkerID: "worker", ThreadID: "thread", CWD: "/work", Namespace: "namespace", LaunchRecordID: "launch"}}, Checkpoint: orchestrator.PrivateCheckpoint{QueuedQueueItemIDs: []string{"queue-1"}}}
	stopped, _ := json.Marshal(personalSourceSealEvidence{Workers: []personalSourceStopProof{{WorkerID: "worker"}}})
	seal := workstore.PersonalMigrationSeal{ScopeID: "scope", SourceHash: migrationBytesHash([]byte("manifest")), Evidence: stopped}
	intent := sourceResumeIntent(source, seal, report)
	if intent == nil || intent.EvidenceVersion != "legacy-codex-v1" || intent.ConnectionIdentity != "" || intent.LegacyEvidenceID != id {
		t.Fatal("versioned legacy provenance rejected")
	}
	evidence.Namespace = "wrong"
	source.Source.Records[1].Data, _ = json.Marshal(evidence)
	if sourceResumeIntent(source, seal, report) != nil {
		t.Fatal("different namespace accepted")
	}
}
