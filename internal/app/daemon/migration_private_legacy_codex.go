package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type legacyCodexSessionEvidence struct {
	Version, ScopeID, WorkerID, ThreadID, CWD, Namespace, ProfileID, SourceFingerprint, LaunchContentID string
	WorkerGeneration, SettingsRevision                                                                  int64
	LaunchSnapshotHash, JournalEventHash, HistoryRelativePath, SessionMetadataHash                      string
	Process                                                                                             agentproto.RuntimeProcessIdentity
}

func legacyCodexEvidenceID(scope, worker string, generation int64) string {
	raw, _ := json.Marshal([]any{scope, worker, generation})
	return "migration-legacy-codex:" + migrationBytesHash(raw)
}

// recordLegacyCodexEvidence uses only original source records, an exact exited
// process, the bound runtime journal and the selected profile's history root.
// It does not assert that any active attempt or external output is settled.
func (c *personalMigrationCoordinator) recordLegacyCodexEvidence(ctx context.Context, scope string, owner privatechat.Owner, now time.Time) error {
	source, err := c.app.capturePersonalMigrationSource(ctx, c.app.teamRuntime.store, scope, c.limits)
	if err != nil {
		return err
	}
	report, err := mapPersonalMigrationSource(source, owner)
	if err != nil {
		return err
	}
	if len(report.History) != 1 || source.Settings.Backend != agentproto.BackendCodex {
		return nil
	}
	h := report.History[0]
	if h.ThreadID == "" || h.Namespace == "" || h.LaunchRecordID == "" || !filepath.IsAbs(h.CWD) {
		return nil
	}
	var worker work.WorkerSession
	var launch workhost.LaunchRecord
	wc, lc := 0, 0
	for _, r := range source.Source.Records {
		if r.Table == "workers" && r.Key == h.WorkerID {
			if json.Unmarshal(r.Payload, &worker) != nil {
				return workstore.ErrConflict
			}
			wc++
		}
		if r.Table == "content" && r.Key == h.LaunchRecordID {
			if json.Unmarshal(r.Data, &launch) != nil {
				return workstore.ErrConflict
			}
			lc++
		}
	}
	if wc != 1 || lc != 1 || worker.NativeSessionID != h.ThreadID || worker.NativeNamespace != h.Namespace || launch.WorkerID != worker.ID || launch.ScopeID != scope || launch.Generation != worker.Generation || launch.Backend != work.BackendCodex || launch.ProfileID != source.Settings.ProfileID || launch.SettingsRevision != source.Settings.SettingsRevision || launch.SettingsRevision < 1 || launch.Snapshot.CWD != h.CWD || launch.Snapshot.CarrierIncarnation != worker.CarrierIncarnation {
		return nil
	}
	hostID := c.app.teamRuntime.codexUpgradeHostID
	if hostID == "" || launch.Snapshot.HostID != hostID {
		return nil
	}
	var process agentproto.RuntimeProcessIdentity
	if err = c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		process, e = tx.GetCarrierProcess(worker.CarrierIncarnation)
		return e
	}); err != nil {
		return err
	}
	if process.HostID != hostID {
		return nil
	}
	probe, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	err = hostinventory.WaitProcessExit(probe, hostinventory.ProcessIdentity{Platform: process.Platform, PID: process.PID, StartIdentity: process.StartIdentity})
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
	launchHash, eventHash, err := runtimejournal.VerifyLegacySessionProvenance(ctx, launch.Snapshot, c.app.headlessRuntime.Paths.StateDir, h.ThreadID)
	if err != nil {
		return nil
	}
	c.app.mu.Lock()
	env, _, _, err := c.app.applyCodexHeadlessProfileConfigLocked(c.app.headlessRuntime.BaseEnv, c.app.headlessRuntime.LaunchArgs, agentproto.BackendCodex, launch.ProfileID, nil)
	c.app.mu.Unlock()
	if err != nil {
		return err
	}
	namespace, err := nativeHistoryNamespace(agentproto.BackendCodex, env, h.CWD, hostID)
	if err != nil {
		return err
	}
	if namespace != h.Namespace {
		return nil
	}
	root, err := nativeHistoryRoot(agentproto.BackendCodex, env, h.CWD)
	if err != nil {
		return err
	}
	relative, metaHash, err := findLegacyCodexSession(ctx, filepath.Join(root, "sessions"), h.ThreadID, h.CWD)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	evidence := legacyCodexSessionEvidence{Version: "legacy-codex-v1", ScopeID: scope, WorkerID: worker.ID, WorkerGeneration: worker.Generation, ThreadID: h.ThreadID, CWD: h.CWD, Namespace: h.Namespace, ProfileID: launch.ProfileID, SettingsRevision: launch.SettingsRevision, SourceFingerprint: source.Settings.SourceFingerprint, LaunchContentID: h.LaunchRecordID, LaunchSnapshotHash: launchHash, JournalEventHash: eventHash, HistoryRelativePath: relative, SessionMetadataHash: metaHash, Process: process}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	return c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.PutContent(work.NewContent(legacyCodexEvidenceID(scope, worker.ID, worker.Generation), scope, "application/json", raw, now))
	})
}

func findLegacyCodexSession(ctx context.Context, root, session, cwd string) (string, string, error) {
	if session == "" || len(session) > 128 || strings.ContainsAny(session, "/\\\x00") || !filepath.IsAbs(root) || !filepath.IsAbs(cwd) {
		return "", "", os.ErrNotExist
	}
	var matches []string
	visited := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		visited++
		if visited > 1000000 {
			return fmt.Errorf("history inventory limit exceeded")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") && strings.Contains(d.Name(), session) {
			matches = append(matches, path)
			if len(matches) > 1 {
				return fs.SkipAll
			}
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return "", "", os.ErrNotExist
	}
	if err != nil {
		return "", "", err
	}
	if len(matches) != 1 {
		return "", "", os.ErrNotExist
	}
	info, err := os.Lstat(matches[0])
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() {
		return "", "", os.ErrNotExist
	}
	f, err := os.Open(matches[0])
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, 4<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for i := 0; i < 20 && scanner.Scan(); i++ {
		raw := append([]byte(nil), scanner.Bytes()...)
		var item struct {
			Type    string `json:"type"`
			Payload struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		if item.Type != "session_meta" {
			continue
		}
		if item.Payload.ID != session || filepath.Clean(item.Payload.CWD) != filepath.Clean(cwd) {
			return "", "", os.ErrNotExist
		}
		relative, err := filepath.Rel(root, matches[0])
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", "", os.ErrNotExist
		}
		return relative, migrationBytesHash(raw), nil
	}
	if err = scanner.Err(); err != nil {
		return "", "", err
	}
	return "", "", os.ErrNotExist
}

func (c *personalMigrationCoordinator) verifyLegacyCodexEvidence(ctx context.Context, source PersonalMigrationSource, seal workstore.PersonalMigrationSeal, plan privatechat.MigrationResumeIntent) error {
	var evidence legacyCodexSessionEvidence
	var launch workhost.LaunchRecord
	var worker work.WorkerSession
	ec, lc, wc := 0, 0, 0
	for _, r := range source.Source.Records {
		if r.Table == "content" && r.Key == plan.LegacyEvidenceID {
			if migrationBytesHash(r.Data) != plan.LegacyEvidenceHash || json.Unmarshal(r.Data, &evidence) != nil {
				return workstore.ErrConflict
			}
			ec++
		}
		if r.Table == "content" && r.Key == plan.LaunchContentID {
			if json.Unmarshal(r.Data, &launch) != nil {
				return workstore.ErrConflict
			}
			lc++
		}
		if r.Table == "workers" && r.Key == plan.WorkerID {
			if json.Unmarshal(r.Payload, &worker) != nil {
				return workstore.ErrConflict
			}
			wc++
		}
	}
	if ec != 1 || lc != 1 || wc != 1 || evidence.Version != "legacy-codex-v1" || evidence.ScopeID != seal.ScopeID || evidence.WorkerID != worker.ID || evidence.WorkerGeneration != worker.Generation || evidence.ThreadID != worker.NativeSessionID || evidence.Namespace != worker.NativeNamespace || evidence.CWD != launch.Snapshot.CWD || evidence.Process.HostID != c.app.teamRuntime.codexUpgradeHostID {
		return workstore.ErrConflict
	}
	var process agentproto.RuntimeProcessIdentity
	if err := c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		process, e = tx.GetCarrierProcess(worker.CarrierIncarnation)
		return e
	}); err != nil {
		return err
	}
	if process != evidence.Process {
		return workstore.ErrConflict
	}
	probe, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	err := hostinventory.WaitProcessExit(probe, hostinventory.ProcessIdentity{Platform: process.Platform, PID: process.PID, StartIdentity: process.StartIdentity})
	cancel()
	if err != nil {
		return err
	}
	launchHash, eventHash, err := runtimejournal.VerifyLegacySessionProvenance(ctx, launch.Snapshot, c.app.headlessRuntime.Paths.StateDir, worker.NativeSessionID)
	if err != nil {
		return err
	}
	if launchHash != evidence.LaunchSnapshotHash || eventHash != evidence.JournalEventHash {
		return workstore.ErrConflict
	}
	c.app.mu.Lock()
	env, _, _, err := c.app.applyCodexHeadlessProfileConfigLocked(c.app.headlessRuntime.BaseEnv, c.app.headlessRuntime.LaunchArgs, agentproto.BackendCodex, launch.ProfileID, nil)
	c.app.mu.Unlock()
	if err != nil {
		return err
	}
	namespace, err := nativeHistoryNamespace(agentproto.BackendCodex, env, launch.Snapshot.CWD, process.HostID)
	if err != nil {
		return err
	}
	if namespace != worker.NativeNamespace {
		return workstore.ErrConflict
	}
	root, err := nativeHistoryRoot(agentproto.BackendCodex, env, launch.Snapshot.CWD)
	if err != nil {
		return err
	}
	relative, metaHash, err := findLegacyCodexSession(ctx, filepath.Join(root, "sessions"), worker.NativeSessionID, launch.Snapshot.CWD)
	if err != nil {
		return err
	}
	if relative != evidence.HistoryRelativePath || metaHash != evidence.SessionMetadataHash {
		return workstore.ErrConflict
	}
	return nil
}
