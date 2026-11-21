package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workmigration"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

const teamMigrationID = "persistent-teams-v1"

// Called under the daemon lock, before any legacy loader normalizes its files.
// The immutable manifest is reused after a crash. No source file is rewritten.
func captureTeamLegacyFiles(stateDir string) (workmigration.Snapshot, error) {
	dir := filepath.Join(stateDir, "migrations", teamMigrationID)
	manifest := filepath.Join(dir, "source.json")
	var snapshot workmigration.Snapshot
	if data, err := os.ReadFile(manifest); err == nil {
		err = json.Unmarshal(data, &snapshot)
		return snapshot, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return snapshot, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return snapshot, err
	}
	snapshot = workmigration.Snapshot{ID: teamMigrationID, Version: workmigration.SnapshotVersion}
	names := []string{"surface-resume-state.json", "feishu-room-primary.json", "bot-capability-settings.json", "goal-interlock-state.json", "profile-context-preferences.json", "claude-workspace-profile-state.json", "feishu-bot-identities.json", "feishu-bot-facts.json", "cron-state.json"}
	previews, err := filepath.Glob(filepath.Join(stateDir, "feishu-md-preview-*.json"))
	if err != nil {
		return snapshot, err
	}
	for _, path := range previews {
		names = append(names, filepath.Base(path))
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := readTeamCaptureFile(filepath.Join(stateDir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return snapshot, err
		}
		hash := sha256.Sum256(data)
		backup := workmigration.Backup{Path: filepath.Join(dir, name), SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data))}
		if err = writeTeamImmutable(backup.Path, data); err != nil {
			return snapshot, err
		}
		snapshot.Backups = append(snapshot.Backups, backup)
		kind := "config"
		if name == "cron-state.json" {
			kind = "cron"
		}
		if len(name) > 18 && name[:18] == "feishu-md-preview-" {
			kind = "preview"
		}
		raw, _ := json.Marshal(struct {
			Filename string
			Backup   workmigration.Backup
		}{name, backup})
		snapshot.Records = append(snapshot.Records, workmigration.LegacyRecord{Key: "file:" + name, UnitKey: "file:" + name, Kind: kind, Raw: raw})
		if name == "surface-resume-state.json" {
			var source struct {
				Version int                        `json:"version"`
				Entries map[string]json.RawMessage `json:"entries"`
			}
			// Unrecognized data stays in its byte-for-byte backup for inspection.
			if json.Unmarshal(data, &source) == nil && source.Version == 1 {
				keys := make([]string, 0, len(source.Entries))
				for key := range source.Entries {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					raw := source.Entries[key]
					chatType := ""
					if ref, ok := feishuidentity.ParseSurfaceRef(key); ok {
						if ref.IsUser() {
							chatType = "p2p"
						} else if ref.IsChat() {
							chatType = "group"
						}
					}
					snapshot.Records = append(snapshot.Records, workmigration.LegacyRecord{Key: "history:" + key, UnitKey: key, Kind: "surface", ChatType: chatType, Raw: raw})
				}
			}
		}
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return snapshot, err
	}
	if err = writeTeamImmutable(manifest, data); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func readTeamCaptureFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return nil, fmt.Errorf("legacy state is not a bounded regular file: %s", path)
	}
	return io.ReadAll(io.LimitReader(f, 64<<20+1))
}

func writeTeamImmutable(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if sha256.Sum256(existing) != sha256.Sum256(data) {
			return fmt.Errorf("migration backup changed: %s", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".capture-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// A hard link publishes without replacing an earlier successful capture.
	if err = os.Link(f.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
