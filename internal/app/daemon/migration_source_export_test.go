package daemon

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersonalMigrationSourceSettingsSnapshot(t *testing.T) {
	ctx := t.Context()
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "app", AppSecret: "bot-secret"}}
	cfg.Claude.Profiles = []config.ClaudeProfileConfig{{ID: "profile", Name: "Profile", AuthMode: config.ClaudeAuthModeAuthToken, AuthToken: "profile-secret", Model: "model-one"}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendClaude, ClaudeProfileID: "profile"})
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = app.syncWorkRoles(ctx, store); err != nil {
		t.Fatal(err)
	}
	roleID := teamID("role", "gateway")
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, RoleID: roleID, PrincipalID: "human", Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	limits := workstore.PersonalExportLimits{MaxRows: 1000, MaxBytes: 4 << 20}
	snapshot, err := app.capturePersonalMigrationSource(ctx, store, "personal", limits)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Settings.ProfileID != "profile" || snapshot.Settings.Model != "model-one" || snapshot.Settings.SourceFingerprint == "" || snapshot.SHA256 == "" {
		t.Fatalf("missing effective settings: %+v", snapshot.Settings)
	}
	if err = snapshot.Verify(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "profile-secret") || strings.Contains(string(raw), "bot-secret") {
		t.Fatal("credentials leaked")
	}
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendClaude, ClaudeProfileID: "profile", PromptOverride: state.ModelConfigRecord{Model: "changed"}})
	if _, err = app.capturePersonalMigrationSource(ctx, store, "personal", limits); !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("stale fingerprint accepted: %v", err)
	}
}
