package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestLegacyCodexHistoryEnvDoesNotProbeOrWriteLaunchFiles(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "CODEX_HOME=" + filepath.Join(t.TempDir(), "history")}
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		t.Fatal("history lookup launched OAuth probe")
		return codexprofile.OAuthProbeObservation{}, nil
	}
	managed := filepath.Join(app.headlessRuntime.Paths.StateDir, "codex-model-catalogs")
	for _, profile := range []string{config.CodexNativeProfileID, config.CodexOAuthProfileID} {
		env, err := app.legacyCodexHistoryEnv(profile)
		if err != nil || !reflect.DeepEqual(env, app.headlessRuntime.BaseEnv) {
			t.Fatalf("profile %s history environment: %v %v", profile, env, err)
		}
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatalf("history lookup created launch files: %v", err)
	}
	if _, err := app.legacyCodexHistoryEnv("missing-profile"); !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("unknown profile accepted: %v", err)
	}
}
