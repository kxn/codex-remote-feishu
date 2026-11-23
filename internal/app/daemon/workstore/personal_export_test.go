package workstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersonalExportBasic(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Within(context.Background(), func(tx *Tx) error {
		if e := tx.PutPrincipal(work.Principal{ID: "person", DisplayName: "Person", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "Work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		return tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, PrincipalID: "person", RoleID: "role", Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Within(t.Context(), func(tx *Tx) error {
		_, e := tx.CapturePersonalExport("personal", PersonalExportLimits{MaxRows: 1000, MaxBytes: 1 << 20})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
}
