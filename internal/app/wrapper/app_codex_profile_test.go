package wrapper

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
)

func TestBuildCodexChildLaunchForwardsDaemonResolvedProfileWithoutReadingConfig(t *testing.T) {
	t.Setenv(codexprofile.CodexProfileAPIKeyEnv, "profile-secret")
	app := New(Config{
		Backend:        "codex",
		CodexProfileID: "team-proxy",
		ConfigPath:     "/missing/app-config.json",
		Source:         "vscode",
	})

	baseArgs := []string{"app-server", "-c", `model_provider="codex_remote_profile_1234"`}
	args, env := app.buildCodexChildLaunch(baseArgs)
	if strings.Join(args, "\x00") != strings.Join(baseArgs, "\x00") {
		t.Fatalf("wrapper changed daemon-resolved args: %#v", args)
	}
	if got := lookupEnv(env, codexprofile.CodexProfileAPIKeyEnv); got != "profile-secret" {
		t.Fatalf("expected profile secret env, got %q", got)
	}
}
