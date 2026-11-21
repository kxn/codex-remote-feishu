package workmigration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func Fingerprint(snapshot Snapshot) (string, error) {
	if snapshot.ID == "" || snapshot.Version != SnapshotVersion || len(snapshot.Records) > 100000 {
		return "", fmt.Errorf("invalid or unsupported migration snapshot")
	}
	seen := map[string]bool{}
	records := append([]LegacyRecord(nil), snapshot.Records...)
	for _, record := range records {
		if record.Key == "" || record.UnitKey == "" || seen[record.Key] || !json.Valid(record.Raw) || len(record.Raw) > 4<<20 {
			return "", fmt.Errorf("migration records require unique IDs, units and bounded source JSON")
		}
		seen[record.Key] = true
		switch record.Kind {
		case "surface", "queue", "native", "cron", "preview", "config":
		default:
			return "", fmt.Errorf("unknown migration record kind")
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Key < records[j].Key })
	type digest struct {
		SHA256 string
		Size   int64
	}
	backups := make([]digest, 0, len(snapshot.Backups))
	for _, backup := range snapshot.Backups {
		if len(backup.SHA256) != 64 || backup.Size < 0 {
			return "", fmt.Errorf("backup digest and size required")
		}
		if _, err := hex.DecodeString(backup.SHA256); err != nil {
			return "", fmt.Errorf("invalid backup digest")
		}
		backups = append(backups, digest{backup.SHA256, backup.Size})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].SHA256 == backups[j].SHA256 {
			return backups[i].Size < backups[j].Size
		}
		return backups[i].SHA256 < backups[j].SHA256
	})
	data, err := json.Marshal(struct {
		Version int
		Records []LegacyRecord
		Backups []digest
	}{snapshot.Version, records, backups})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func (c *Coordinator) verifyBackup(backup Backup) error {
	root, err := filepath.EvalSymlinks(c.options.StateDir)
	if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(backup.Path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, actual)
	if err != nil || !filepath.IsAbs(backup.Path) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("migration backup must be in the controlled host state directory")
	}
	original, err := os.Lstat(backup.Path)
	if err != nil || !original.Mode().IsRegular() {
		return fmt.Errorf("migration backup must be a regular file")
	}
	file, err := os.Open(backup.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(original, opened) || opened.Size() != backup.Size || (runtime.GOOS != "windows" && opened.Mode().Perm()&0077 != 0) {
		return fmt.Errorf("migration backup identity, size or owner-only permissions changed")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return err
	}
	if size != backup.Size || hex.EncodeToString(hash.Sum(nil)) != backup.SHA256 {
		return fmt.Errorf("migration backup hash changed")
	}
	return nil
}
func stableID(prefix string, parts ...string) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return prefix + "_" + hex.EncodeToString(sum[:20])
}
func nativeResourceKey(native LegacyNative) string {
	return workstore.MigrationNativeResourceKey(string(native.Backend), native.Namespace, native.SessionID)
}
