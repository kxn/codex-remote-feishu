package daemon

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestLegacyHistoryLaunchUsesExactSealedCarrier(t *testing.T) {
	owner := agentproto.ExecutionBinding{ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1}
	carrier := workstore.CarrierRegistration{Incarnation: "carrier", Owner: owner, Backend: work.BackendCodex}
	worker := work.WorkerSession{ID: "worker", ScopeID: "scope", ContextID: "context", RoleID: "role", Backend: work.BackendCodex, NativeNamespace: "codex"}
	launch := workhost.LaunchRecord{WorkerID: "worker", ScopeID: "scope", Generation: 1, Backend: work.BackendCodex, ProfileID: "profile", Snapshot: agentproto.RuntimeLaunchSnapshot{CWD: "/workspace", Owner: owner, CarrierIncarnation: "carrier"}}
	op := work.Operation{Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, RequestRef: "launch"}
	wr, _ := json.Marshal(worker)
	lr, _ := json.Marshal(launch)
	or, _ := json.Marshal(op)
	nr, _ := json.Marshal(work.PrivateNavigation{PersonalScopeID: "scope", RoleID: "role", FrontContextID: "context"})
	cr, _ := json.Marshal(work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "role", Purpose: work.PurposeBusiness})
	source := PersonalMigrationSource{Settings: PersonalMigrationSettings{ProfileID: "profile"}, Source: workstore.PersonalExport{ScopeID: "scope", Records: []workstore.PersonalExportRecord{{Table: "private_navigation", Key: "nav", Payload: nr}, {Table: "contexts", Key: "context", Payload: cr}, {Table: "workers", Key: "worker", Payload: wr}, {Table: "operations", Key: "op", Payload: or}, {Table: "content", Key: "launch", Data: lr}}}}
	if _, err := legacyHistoryLaunch(source, carrier); err != nil {
		t.Fatal(err)
	}
	bad := carrier
	bad.Owner.ScopeID = "other"
	if _, err := legacyHistoryLaunch(source, bad); err == nil {
		t.Fatal("other source scope accepted")
	}
	bad = carrier
	bad.Incarnation = "other"
	if _, err := legacyHistoryLaunch(source, bad); err == nil {
		t.Fatal("other carrier accepted")
	}
	bad = carrier
	bad.Owner.SessionGeneration++
	if _, err := legacyHistoryLaunch(source, bad); err == nil {
		t.Fatal("other generation accepted")
	}
	source.Settings.ProfileID = "other"
	if _, err := legacyHistoryLaunch(source, carrier); err == nil {
		t.Fatal("other profile accepted")
	}
}

func TestLegacyHistoryAliasUsesRealJournalAndCurrentExactMetadata(t *testing.T) {
	app, _ := newApplicationRuntimeTestApp(t)
	root := t.TempDir()
	cwd := t.TempDir()
	stateRoot := app.headlessRuntime.Paths.StateDir
	app.headlessRuntime.BaseEnv = []string{"HOME=" + root}
	host := "test-host"
	app.teamRuntime.codexUpgradeHostID = host
	owner := agentproto.ExecutionBinding{Version: agentproto.WorkerProtocolVersion, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	path := filepath.Join(stateRoot, "alias-test-runtime.db")
	snapshot := agentproto.RuntimeLaunchSnapshot{Version: agentproto.WorkerProtocolVersion, Owner: owner, CWD: cwd, JournalPath: path, CarrierIncarnation: "carrier", HostID: host}
	journal, err := runtimejournal.Open(t.Context(), path, stateRoot, cwd, "carrier")
	if err != nil {
		t.Fatal(err)
	}
	if err = journal.BindLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = journal.PersistEvents([]agentproto.Event{{Kind: agentproto.EventThreadDiscovered, ThreadID: "session", Execution: &owner}}); err != nil {
		t.Fatal(err)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".codex", "sessions", "2026")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": "session", "cwd": cwd}})
	file := filepath.Join(dir, "rollout-session.jsonl")
	if err = os.WriteFile(file, append(meta, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	worker := work.WorkerSession{ID: "worker", ScopeID: "scope", ContextID: "context", RoleID: "role", Backend: work.BackendCodex, NativeNamespace: "codex"}
	launch := workhost.LaunchRecord{WorkerID: "worker", ScopeID: "scope", Generation: 1, Backend: work.BackendCodex, ProfileID: state.OAuthCodexProfileID, Snapshot: snapshot}
	op := work.Operation{Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, RequestRef: "launch"}
	wr, _ := json.Marshal(worker)
	lr, _ := json.Marshal(launch)
	or, _ := json.Marshal(op)
	nr, _ := json.Marshal(work.PrivateNavigation{PersonalScopeID: "scope", RoleID: "role", FrontContextID: "context"})
	cr, _ := json.Marshal(work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "role", Purpose: work.PurposeBusiness})
	source := PersonalMigrationSource{Settings: PersonalMigrationSettings{ProfileID: state.OAuthCodexProfileID}, Source: workstore.PersonalExport{ScopeID: "scope", Records: []workstore.PersonalExportRecord{{Table: "private_navigation", Key: "nav", Payload: nr}, {Table: "contexts", Key: "context", Payload: cr}, {Table: "workers", Key: "worker", Payload: wr}, {Table: "operations", Key: "op", Payload: or}, {Table: "content", Key: "launch", Data: lr}}}}
	carrier := workstore.CarrierRegistration{Incarnation: "carrier", Owner: owner, Backend: work.BackendCodex}
	namespace, err := nativeHistoryNamespace(agentproto.BackendCodex, app.headlessRuntime.BaseEnv, cwd, host)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := app.proveLegacyPrivateHistory(t.Context(), source, carrier, namespace, "session")
	if err != nil || alias.Version != 1 {
		t.Fatalf("exact evidence: %+v %v", alias, err)
	}
	if _, err = app.proveLegacyPrivateHistory(t.Context(), source, carrier, namespace, "other"); err == nil {
		t.Fatal("other session accepted")
	}
	if _, err = app.proveLegacyPrivateHistory(t.Context(), source, carrier, "history_other", "session"); err == nil {
		t.Fatal("other root accepted")
	}
	if err = os.WriteFile(filepath.Join(dir, "duplicate-session.jsonl"), append(meta, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = app.proveLegacyPrivateHistory(t.Context(), source, carrier, namespace, "session"); err == nil {
		t.Fatal("ambiguous current metadata accepted")
	}
}

// TestLegacyHistoryReadOnlyProbe is opt-in and never writes the inspected DBs.
// It reports aggregate counts only; callers supply the daemon state directory.
func TestLegacyHistoryReadOnlyProbe(t *testing.T) {
	root := os.Getenv("PRIVATE_ALIAS_PROBE_STATE")
	if root == "" {
		t.Skip("opt-in read-only probe")
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(root, "team-work.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw []byte
	if err = db.QueryRow("SELECT manifest FROM personal_migration_seals LIMIT 1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var source PersonalMigrationSource
	if json.Unmarshal(raw, &source) != nil || source.Verify() != nil {
		t.Fatal("invalid sealed source")
	}
	rows, err := db.Query(`SELECT session_id FROM native_turns WHERE session_id<>'' UNION SELECT json_extract(payload,'$.native_session_id') FROM workers WHERE coalesce(json_extract(payload,'$.native_session_id'),'')<>''`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	app, _ := newApplicationRuntimeTestApp(t)
	app.headlessRuntime.Paths.StateDir = root
	home := os.Getenv("PRIVATE_ALIAS_PROBE_HOME")
	if !filepath.IsAbs(home) {
		t.Fatal("explicit absolute probe home required")
	}
	app.headlessRuntime.BaseEnv = []string{"HOME=" + home}
	accepted, rejected := 0, 0
	stages := map[string]int{}
	for _, id := range ids {
		rows, err = db.Query(`SELECT DISTINCT c.payload FROM carrier_registrations c LEFT JOIN native_turns n ON n.carrier_incarnation=c.incarnation LEFT JOIN workers w ON w.id=c.worker_id WHERE n.session_id=? OR (json_extract(w.payload,'$.native_session_id')=? AND json_extract(w.payload,'$.carrier_incarnation')=c.incarnation)`, id, id)
		if err != nil {
			t.Fatal(err)
		}
		var carriers []workstore.CarrierRegistration
		for rows.Next() {
			var b []byte
			var carrier workstore.CarrierRegistration
			if rows.Scan(&b) != nil || json.Unmarshal(b, &carrier) != nil {
				t.Fatal("invalid carrier")
			}
			carriers = append(carriers, carrier)
		}
		rows.Close()
		if len(carriers) == 0 {
			rejected++
			continue
		}
		valid := true
		for _, carrier := range carriers {
			if carrier.Backend != work.BackendCodex || carrier.Owner.ScopeID != source.Source.ScopeID {
				stages["foreign"]++
				valid = false
				break
			}
			launch, e := legacyHistoryLaunch(source, carrier)
			if e != nil {
				stages["launch"]++
				valid = false
				break
			}
			app.teamRuntime.codexUpgradeHostID = launch.Snapshot.HostID
			if _, _, probeErr := runtimejournal.VerifyLegacySessionProvenance(t.Context(), launch.Snapshot, root, id); probeErr != nil {
				if errors.Is(probeErr, os.ErrNotExist) {
					stages["journal_file_missing"]++
				} else if errors.Is(probeErr, sql.ErrNoRows) {
					stages["journal_row_missing"]++
				} else {
					stages["journal_invalid"]++
				}
				valid = false
				break
			}
			if _, _, probeErr := findLegacyCodexSession(t.Context(), filepath.Join(home, ".codex", "sessions"), id, launch.Snapshot.CWD); probeErr != nil {
				count := 0
				_ = filepath.WalkDir(filepath.Join(home, ".codex", "sessions"), func(path string, d os.DirEntry, e error) error {
					if e == nil && !d.IsDir() && strings.Contains(d.Name(), id) && strings.HasSuffix(d.Name(), ".jsonl") {
						count++
					}
					return nil
				})
				if count == 0 {
					stages["metadata_filename_missing"]++
				} else if count > 1 {
					stages["metadata_filename_ambiguous"]++
				} else {
					stages["metadata_content_mismatch"]++
				}
				stages["metadata_probe_failed"]++
				valid = false
				break
			}
			ns, e := nativeHistoryNamespace(agentproto.BackendCodex, app.headlessRuntime.BaseEnv, launch.Snapshot.CWD, launch.Snapshot.HostID)
			if e != nil {
				valid = false
				break
			}
			if _, e = app.proveLegacyPrivateHistory(t.Context(), source, carrier, ns, id); e != nil {
				switch {
				case errors.Is(e, sql.ErrNoRows):
					stages["journal_no_matching_event"]++
				case errors.Is(e, os.ErrNotExist):
					stages["metadata_missing"]++
				case errors.Is(e, runtimejournal.ErrConflict):
					stages["journal_conflict"]++
				default:
					stages["other"]++
					t.Logf("error type %T", e)
				}
				valid = false
				break
			}
		}
		if valid {
			accepted++
		} else {
			rejected++
		}
	}
	t.Logf("exact_sealed_launch_journal_metadata=%d rejected=%d stages=%v; resume and committed-owner proof not evaluated", accepted, rejected, stages)
}

func TestLegacyHistoryResumeRequiresExactCarrierAndAllDomains(t *testing.T) {
	carrier := workstore.CarrierRegistration{Incarnation: "carrier", Owner: agentproto.ExecutionBinding{ScopeID: "scope", ContextID: "context", WorkerSessionID: "worker", SessionGeneration: 2, ControllerGeneration: 3}}
	isolation := workstore.PersonalExecutionIsolation{Worker: work.WorkerSession{ID: "worker", ScopeID: "scope", ContextID: "context", Generation: 2, ControllerGeneration: 3, CarrierIncarnation: "carrier"}, ProcessScopesStopped: true, NativeCommandsIdle: true, InputsIdle: true, ToolsIdle: true, GoalsIdle: true, TasksIdle: true, TurnsTerminal: true, UncertaintiesEmpty: true}
	encode := func(v workstore.PersonalExecutionIsolation) []byte {
		b, _ := json.Marshal(personalSourceSealEvidence{Workers: []personalSourceStopProof{{WorkerID: "worker", Isolated: &v}}})
		return b
	}
	if !legacyCarrierQuiesced(encode(isolation), carrier) {
		t.Fatal("exact quiescence rejected")
	}
	old := carrier
	old.Owner.SessionGeneration--
	if legacyCarrierQuiesced(encode(isolation), old) {
		t.Fatal("later worker generation used to prove older carrier")
	}
	old = carrier
	old.Incarnation = "old"
	if legacyCarrierQuiesced(encode(isolation), old) {
		t.Fatal("other carrier accepted")
	}
	isolation.InputsIdle = false
	if legacyCarrierQuiesced(encode(isolation), carrier) {
		t.Fatal("unknown input declared safe")
	}
}

func TestLegacyHistoryUsesConfirmedBotProfileAndDoesNotRescanPickerDraft(t *testing.T) {
	cases := []struct{ record, checkpoint, want string }{{"cp_oauth", "", "cp_oauth"}, {"cp_custom", "cp_native", "cp_custom"}, {"", "cp_oauth", "cp_oauth"}}
	for _, v := range cases {
		if got := effectiveLegacyCodexProfile(state.BotCapabilitySettingsRecord{CodexProfileID: v.record}, v.checkpoint); got != v.want {
			t.Fatalf("profile=%q want=%q", got, v.want)
		}
	}
	for _, kind := range []control.ActionKind{control.ActionTargetPickerSelectSession, control.ActionTargetPickerSelectWorkspace, control.ActionTargetPickerPage, control.ActionTargetPickerConfirm, control.ActionThreadSelectionPage} {
		raw, _ := json.Marshal(control.Action{Kind: kind})
		if historyAliasNeedsRefresh(raw) {
			t.Fatalf("draft %s rescans", kind)
		}
	}
	raw, _ := json.Marshal(control.Action{Kind: control.ActionShowThreads})
	if !historyAliasNeedsRefresh(raw) {
		t.Fatal("explicit list did not refresh")
	}
}
