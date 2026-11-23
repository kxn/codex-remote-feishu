package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateProfilesProjectOnlyUnderProcessor(t *testing.T) {
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app := privatechat.New(nil, orchestrator.Config{})
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privatechat.CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	host := &App{service: app.Service()}
	host.privateServiceOwned.Store(true)
	records := []state.CodexProfileSummary{{ID: "cp_test", Name: "test", Available: true}}
	host.mu.Lock()
	host.updatePrivateCodexCatalogLocked(records)
	host.mu.Unlock()
	records[0].Name = "mutated"
	for _, v := range app.Service().CodexProfiles() {
		if v.ID == "cp_test" {
			t.Fatal("host wrote private core directly")
		}
	}
	if err = host.refreshPrivateProfileCatalog(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for _, v := range app.Service().CodexProfiles() {
		if v.ID == "cp_test" {
			if v.Name != "test" {
				t.Fatalf("snapshot aliased: %#v", v)
			}
			host.mu.Lock()
			host.syncCodexProfilesCatalogLocked(config.AppConfig{})
			host.mu.Unlock()
			still := false
			for _, item := range app.Service().CodexProfiles() {
				if item.ID == "cp_test" {
					still = true
				}
			}
			if !still {
				t.Fatal("catalog sync mutated core outside processor")
			}
			if err = host.refreshPrivateProfileCatalog(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			for _, item := range app.Service().CodexProfiles() {
				if item.ID == "cp_test" {
					t.Fatal("new catalog was not projected")
				}
			}
			return
		}
	}
	t.Fatal("catalog did not reach private core")
}
