package workpractices

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"path/filepath"
	"testing"
)

func TestShippedMethodsSurviveRestartWithoutAdoptingProjectProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work.db")
	store, err := workstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err = Install(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = workstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = Install(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		if e := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRole(work.Role{ID: "role", Name: "Lead", Description: "Lead", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "private", Kind: work.ScopePersonal, PrincipalID: "human", RoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		read := work.ReadScope{ScopeID: "private", RoleID: "role"}
		definitions, e := tx.ListPracticesFor(read)
		if e != nil {
			return e
		}
		if len(definitions) != 5 {
			t.Fatalf("method count %d", len(definitions))
		}
		for _, definition := range definitions {
			_, body, e := tx.GetPracticeFor(read, definition.Owner, definition.PracticeRef)
			if e != nil {
				return e
			}
			if body.ScopeID != ScopeID || len(body.Data) == 0 {
				t.Fatal("library body borrowed a person's storage")
			}
			if e = tx.InstallBuiltinPractice(definition, []byte("changed under same version"), body.CreatedAt); e == nil {
				t.Fatal("immutable shipped version changed")
			}
		}
		adopted, e := tx.ListPracticeAdoptions("private")
		if e != nil {
			return e
		}
		if len(adopted) != 0 {
			t.Fatal("installing library imposed a workflow")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
