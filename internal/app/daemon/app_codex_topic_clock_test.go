package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"testing"
	"time"
)

func TestLegacyRestoreFreezesCodexSettingClockBeforeRouteWrite(t *testing.T) {
	at := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store := surfaceresume.NewStore(surfaceresume.StatePath(dir))
	entry := surfaceresume.Entry{SurfaceSessionID: "feishu:app-1:user:ou_user", GatewayID: "app-1", ChatID: "chat", ActorUserID: "ou_user", ProductMode: "normal", Backend: "codex", CodexModelOverride: "gpt-6-sol", UpdatedAt: at}
	if err := store.Put(entry); err != nil {
		t.Fatal(err)
	}
	app := newRestoreHintTestApp(dir)
	surface := app.service.Surface(entry.SurfaceSessionID)
	if surface == nil || !surface.CodexPromptOverrideUpdatedAt.Equal(at) {
		t.Fatalf("legacy restore did not capture historical setting clock: %#v", surface)
	}
	app.mu.Lock()
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()
	reloaded, err := surfaceresume.LoadStore(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.Get(entry.SurfaceSessionID)
	if !got.CodexPromptOverrideUpdatedAt.Equal(at) || !got.UpdatedAt.After(at) {
		t.Fatalf("route write moved setting clock: %#v", got)
	}
	alias := entry
	alias.SurfaceSessionID = "feishu:app-1:user:legacy"
	alias.ActorUserID = "legacy"
	alias.CodexModelOverride = ""
	alias.CodexPromptOverrideUpdatedAt = at.Add(time.Hour)
	alias.UpdatedAt = alias.CodexPromptOverrideUpdatedAt
	merged, _ := surfaceresume.CanonicalizeEntries(map[string]surfaceresume.Entry{got.SurfaceSessionID: got, alias.SurfaceSessionID: alias})
	if chosen := merged[entry.SurfaceSessionID]; chosen.CodexModelOverride != "" || !chosen.CodexPromptOverrideUpdatedAt.Equal(alias.CodexPromptOverrideUpdatedAt) {
		t.Fatalf("route write revived old setting over newer clear: %#v", chosen)
	}
}

func TestLegacyEmptyOverrideDoesNotBecomeClearOnRestore(t *testing.T) {
	dir := t.TempDir()
	store := surfaceresume.NewStore(surfaceresume.StatePath(dir))
	entry := surfaceresume.Entry{SurfaceSessionID: "feishu:app-1:user:ou_user", GatewayID: "app-1", ChatID: "chat", ActorUserID: "ou_user", ProductMode: "normal", Backend: "codex", UpdatedAt: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)}
	if err := store.Put(entry); err != nil {
		t.Fatal(err)
	}
	app := newRestoreHintTestApp(dir)
	surface := app.service.Surface(entry.SurfaceSessionID)
	if surface == nil || !surface.CodexPromptOverrideUpdatedAt.IsZero() {
		t.Fatalf("missing old fields became explicit clear: %#v", surface)
	}
}
