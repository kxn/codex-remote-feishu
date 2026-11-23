package privatechat

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexReferencesIncludeUnloadedRecoveryHolds(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	cp, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	ref := state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: "profile", Revision: 2}, ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: "profile", Revision: 3}}
	raw, _ := json.Marshal(map[string]any{"nested": map[string]any{"codex_admission_ref": ref}})
	cp.Holds = []orchestrator.PrivateRecoveryHold{{Kind: "pending_launch", Frozen: raw}}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: cp}, 0, nil); err != nil {
		t.Fatal(err)
	}
	refs, err := j.CodexReferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Kind != "recovery_hold" || refs[0].Admission.ProfileRef.Revision != 2 || refs[0].Admission.ContextPreferenceRef.Revision != 3 {
		t.Fatalf("missing durable references: %#v", refs)
	}
}
