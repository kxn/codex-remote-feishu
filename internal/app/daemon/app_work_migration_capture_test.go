package daemon

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workmigration"
)

func TestTeamMigrationCapturesOriginalHistoryWithoutInventingLiveNative(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"version":1,"entries":{"feishu:main:user:ou_human":{"surfaceSessionID":"feishu:main:user:ou_human","actorUserID":"ou_human","resumeThreadID":"native-history","resumeThreadCWD":"/old/repository","extra_preserved_field":42}}}`)
	path := filepath.Join(dir, "surface-resume-state.json")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := captureTeamLegacyFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Backups) != 1 {
		t.Fatal(snapshot)
	}
	backup, err := os.ReadFile(snapshot.Backups[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatal("source bytes changed")
	}
	histories := 0
	for _, record := range snapshot.Records {
		if record.Native != nil {
			t.Fatal("selected history was declared live")
		}
		if record.Kind == "surface" {
			histories++
			if record.ChatType != "p2p" || record.Owner != nil || !bytes.Contains(record.Raw, []byte("extra_preserved_field")) {
				t.Fatal(record)
			}
		}
	}
	if histories != 1 {
		t.Fatal(histories)
	}
	if _, err = workmigration.Fingerprint(snapshot); err != nil {
		t.Fatal(err)
	}
	// A crash after capture must reuse the first original, even if a legacy
	// normalizer subsequently changes the source file.
	if err = os.WriteFile(path, []byte(`{"version":1,"entries":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	again, err := captureTeamLegacyFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(snapshot)
	second, _ := json.Marshal(again)
	if !bytes.Equal(first, second) {
		t.Fatal("restart recaptured and lost old history")
	}
}

func TestTeamMigrationRetainsMalformedStateAndRetiresCronReadOnly(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"surface-resume-state.json": "broken legacy JSON", "cron-state.json": `{"jobs":[{"enabled":true}]}`, "feishu-md-preview-main.json": `{"temporary":true}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := captureTeamLegacyFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Backups) != 3 {
		t.Fatal(snapshot)
	}
	kinds := map[string]int{}
	for _, record := range snapshot.Records {
		kinds[record.Kind]++
		if record.Queue != nil || record.Native != nil {
			t.Fatal("invented executable state")
		}
	}
	if kinds["cron"] != 1 || kinds["preview"] != 1 || kinds["surface"] != 0 {
		t.Fatal(kinds)
	}
	if _, err = workmigration.Fingerprint(snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestTeamMigrationCannotOverwriteImmutableBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.json")
	if err := writeTeamImmutable(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := writeTeamImmutable(path, []byte("replacement")); err == nil {
		t.Fatal("overwrote original")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "original" {
		t.Fatal(string(body), err)
	}
}
