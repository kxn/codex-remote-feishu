package workstore

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestActiveProfileLaunchReferencesUseExactScopeOwnedLaunch(t *testing.T) {
	ctx := t.Context()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if err = s.Within(ctx, func(tx *Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "role", Description: "role", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutPrincipal(work.Principal{ID: "person", DisplayName: "person", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, PrincipalID: "person", RoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutContext(work.WorkContext{ID: "ctx", ScopeID: "scope", RoleID: "role", Workstream: "work", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutWorker(work.WorkerSession{ID: "worker", ScopeID: "scope", ContextID: "ctx", RoleID: "role", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		raw, _ := json.Marshal(map[string]any{"worker_id": "worker", "scope_id": "scope", "generation": 1, "backend": "codex", "profile_id": "selected", "settings_revision": 4})
		body := work.NewContent("launch", "scope", "application/json", raw, now)
		if e := tx.PutContent(body); e != nil {
			return e
		}
		_, _, e := tx.PutOperation(work.Operation{ID: "operation", ScopeID: "scope", Actor: work.Actor{Kind: work.ActorSystem, ID: "host"}, Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "launch", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationConfirmed, ExternalReceipt: "receipt", Revision: 1}, 0)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Within(ctx, func(tx *Tx) error {
		refs, e := tx.ListActiveProfileLaunchReferences(work.BackendCodex, "selected")
		if e != nil {
			return e
		}
		if len(refs) != 1 || refs[0].WorkerID != "worker" || refs[0].SettingsRevision != 4 {
			t.Fatal("active exact launch missing")
		}
		refs, e = tx.ListActiveProfileLaunchReferences(work.BackendCodex, "other")
		if e != nil {
			return e
		}
		if len(refs) != 0 {
			t.Fatal("unrelated profile referenced")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
