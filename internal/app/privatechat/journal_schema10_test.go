package privatechat

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSchema10PreservesControlActionSourceOnReopen(t *testing.T) {
	path := os.Getenv("PRIVATE_SCHEMA10_FIXTURE")
	if path == "" {
		path = filepath.Join(t.TempDir(), "control.db")
	} else if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("compatibility fixture path must not exist")
	}
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	action := privateAction(o, control.ActionReviewApply)
	action.MessageID = "review-result-card"
	raw, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	seq, _, err := j.Admit(t.Context(), Ingress{Owner: o, Generation: 1, EventID: "verified-apply", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	cp, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	cp.QueueItems = map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", SurfaceSessionID: o.SurfaceID, ActorUserID: o.UserID, SourceKind: state.QueueItemSourceControlAction, ControlIngressEventID: "verified-apply", ReplyToMessageID: action.MessageID, Status: state.QueueItemQueued}}
	cp.QueuedQueueItemIDs = []string{"queue-1"}
	cp.NextQueueID = 1
	if err = j.commitCheckpoint(t.Context(), Checkpoint{Owner: o, Generation: 1, Revision: 1, Cursor: seq, Core: cp}, 0, nil, seq, nil); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if _, err = verifyControlInput(t.Context(), j.db, o, 1, "verified-apply", 0, true); err != nil {
		t.Fatal(err)
	}
	var saved []byte
	if err = j.db.QueryRow(`SELECT data FROM private_checkpoint`).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	var restored orchestrator.PrivateCheckpoint
	if err = json.Unmarshal(saved, &restored); err != nil {
		t.Fatal(err)
	}
	item := restored.QueueItems["queue-1"]
	if item == nil || item.SourceKind != state.QueueItemSourceControlAction || item.ControlIngressEventID != "verified-apply" || item.ReplyToMessageID != "review-result-card" {
		t.Fatal("control authority and card reply target were not preserved separately")
	}
}

func TestSchema10PreservesV9InputAndCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	payload := []byte(`{"MessageID":"original","Text":"keep original bytes"}`)
	seq, _, err := j.Admit(t.Context(), Ingress{Owner: o, Generation: 1, EventID: "input", Lane: "input", Payload: payload, Sources: []string{"original"}})
	if err != nil {
		t.Fatal(err)
	}
	a := New(nil, orchestrator.Config{})
	if err = a.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	cp, err := a.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	cp.QueueItems = map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", SurfaceSessionID: o.SurfaceID, ActorUserID: o.UserID, SourceKind: state.QueueItemSourceUser, SourceMessageID: "original", SourceMessageIDs: []string{"original"}, Status: state.QueueItemQueued}}
	cp.QueuedQueueItemIDs = []string{"queue-1"}
	cp.NextQueueID = 1
	if err = j.CommitCheckpoint(t.Context(), Checkpoint{Owner: o, Generation: 1, Revision: 1, Cursor: seq, Core: cp}, 0, nil); err != nil {
		t.Fatal(err)
	}
	var before []byte
	var digest string
	if err = j.db.QueryRow(`SELECT data,digest FROM private_checkpoint`).Scan(&before, &digest); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`PRAGMA user_version=9`); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var version int
	if err = j.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != privateJournalSchemaVersion {
		t.Fatalf("schema=%d error=%v", version, err)
	}
	var after, savedInput []byte
	var savedDigest string
	if err = j.db.QueryRow(`SELECT data,digest FROM private_checkpoint`).Scan(&after, &savedDigest); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || digest != savedDigest {
		t.Fatal("migration rewrote checkpoint or its integrity digest")
	}
	if err = j.db.QueryRow(`SELECT payload FROM ingress WHERE seq=?`, seq).Scan(&savedInput); err != nil || !bytes.Equal(savedInput, payload) {
		t.Fatalf("original input changed: %v", err)
	}
}

func TestJournalRejectsFutureReaderFormatWithoutDowngrading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`PRAGMA user_version=11`); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, e := OpenJournal(path); e == nil {
		opened.Close()
		t.Fatal("future reader contract was accepted")
	} else if !strings.Contains(e.Error(), "unsupported private journal schema") {
		t.Fatal(e)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 11 {
		t.Fatalf("future format mutated: schema=%d error=%v", version, err)
	}
}
