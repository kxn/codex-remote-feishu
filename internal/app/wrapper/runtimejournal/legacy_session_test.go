package runtimejournal

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestVerifyLegacySessionUsesExactLaunchAndOwner(t *testing.T) {
	ctx := t.Context()
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "journal.db")
	s := openTestStore(t, path, root, cwd, "carrier")
	owner := *testCommand().Execution
	snapshot := agentproto.RuntimeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path, CarrierIncarnation: "carrier"}
	if err := s.BindLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventThreadDiscovered, ThreadID: "thread", Execution: &owner}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	launch, event, err := VerifyLegacySessionProvenance(ctx, snapshot, root, "thread")
	if err != nil || launch == "" || event == "" {
		t.Fatalf("exact source: %v", err)
	}
	if _, _, err = VerifyLegacySessionProvenance(ctx, snapshot, root, "other"); err == nil {
		t.Fatal("other session accepted")
	}
	wrong := snapshot
	wrong.Owner.WorkerSessionID = "other"
	if _, _, err = VerifyLegacySessionProvenance(ctx, wrong, root, "thread"); err == nil {
		t.Fatal("other owner accepted")
	}
}
