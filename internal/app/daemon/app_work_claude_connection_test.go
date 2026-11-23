package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestClaudeConnectionIdentityStableAcrossRefreshAndMutableSettings(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".claude.json"), `{"oauthAccount":{"accountUuid":"account-a","emailAddress":"a@example.test","organizationUuid":"org"}}`)
	credentials := filepath.Join(home, ".claude", ".credentials.json")
	write(credentials, `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-refresh","expiresAt":1}}`)
	env := []string{"HOME=" + home, "PATH=/first", config.ClaudeModelEnv + "=old-model"}
	profile := config.BuiltInClaudeProfile()
	first, err := teamClaudeConnectionIdentity(env, profile)
	if err != nil {
		t.Fatal(err)
	}
	write(credentials, `{"claudeAiOauth":{"accessToken":"new","refreshToken":"new-refresh","expiresAt":2}}`)
	profile.Model = "new-model"
	profile.ReasoningEffort = "high"
	profile.Instruction = "new prompt"
	second, err := teamClaudeConnectionIdentity([]string{"HOME=" + home, "PATH=/second", config.ClaudeModelEnv + "=other-model", "CODEX_REMOTE_WORK_SESSION_TOKEN=ephemeral"}, profile)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("token refresh/model/prompt changed account identity")
	}
	write(filepath.Join(home, ".claude.json"), `{"oauthAccount":{"accountUuid":"account-b","emailAddress":"b@example.test","organizationUuid":"org"}}`)
	switched, err := teamClaudeConnectionIdentity(env, profile)
	if err != nil {
		t.Fatal(err)
	}
	if switched == first {
		t.Fatal("account switch reused history")
	}
	routed, err := teamClaudeConnectionIdentity(append(env, config.ClaudeBaseURLEnv+"=https://other.example.test"), profile)
	if err != nil {
		t.Fatal(err)
	}
	if routed == switched {
		t.Fatal("route switch reused history")
	}
}

func TestClaudeConnectionUnidentifiedCredentialsStayConservative(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", ".credentials.json")
	env := []string{"HOME=" + home}
	if err := os.WriteFile(path, []byte(`{"claudeAiOauth":{"accessToken":"a"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := teamClaudeConnectionIdentity(env, config.BuiltInClaudeProfile())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"claudeAiOauth":{"accessToken":"b"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := teamClaudeConnectionIdentity(env, config.BuiltInClaudeProfile())
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("unidentified credential change assumed same account")
	}
}

func TestClaudeConnectionNativeSettingsEffectivePrecedence(t *testing.T) {
	home, cwd, managed := t.TempDir(), t.TempDir(), t.TempDir()
	userDir := filepath.Join(home, ".claude")
	write := func(dir, name, body string) {
		t.Helper()
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(userDir, "settings.json", `{"env":{"ANTHROPIC_BASE_URL":"user","ANTHROPIC_AUTH_TOKEN":"user-key","AWS_REGION":"unused"}}`)
	write(filepath.Join(cwd, ".claude"), "settings.json", `{"env":{"ANTHROPIC_BASE_URL":"project"}}`)
	write(filepath.Join(cwd, ".claude"), "settings.local.json", `{"env":{"ANTHROPIC_BASE_URL":"local","CLAUDE_CODE_OAUTH_TOKEN":"ignored-project-signin"}}`)
	values := map[string]string{"HOME": home, "ANTHROPIC_BASE_URL": "shell", "ANTHROPIC_AUTH_TOKEN": "shell-key"}
	profile := config.BuiltInClaudeProfile()
	resolve := func() claudeConnectionSettings {
		t.Helper()
		s, e := effectiveClaudeConnectionSettings(values, profile, userDir, cwd, managed)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	if s := resolve(); s.Env[config.ClaudeBaseURLEnv] != "local" || s.Env[config.ClaudeAuthTokenEnv] != "user-key" || s.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "" {
		t.Fatal("native project/local and restricted signin precedence incorrect")
	}
	profile = config.ClaudeProfile{ClaudeProfileConfig: config.ClaudeProfileConfig{ID: "api", BaseURL: "profile", AuthMode: config.ClaudeAuthModeAuthToken, AuthToken: "profile-key"}}
	if s := resolve(); s.Env[config.ClaudeBaseURLEnv] != "profile" || s.Env[config.ClaudeAuthTokenEnv] != "profile-key" {
		t.Fatal("profile --settings overlay did not win")
	}
	write(managed, "managed-settings.json", `{"env":{"ANTHROPIC_BASE_URL":"managed","ANTHROPIC_AUTH_TOKEN":"managed-key"}}`)
	write(filepath.Join(managed, "managed-settings.d"), "10-base.json", `{"env":{"ANTHROPIC_BASE_URL":"dropin-first"}}`)
	write(filepath.Join(managed, "managed-settings.d"), "20-final.json", `{"env":{"ANTHROPIC_BASE_URL":"dropin-final"}}`)
	write(filepath.Join(managed, "managed-settings.d"), ".hidden.json", `{"env":{"ANTHROPIC_BASE_URL":"ignored"}}`)
	if s := resolve(); s.Env[config.ClaudeBaseURLEnv] != "dropin-final" || s.Env[config.ClaudeAuthTokenEnv] != "managed-key" {
		t.Fatal("managed priority/dropin order incorrect")
	}
	write(filepath.Join(managed, "managed-settings.d"), "30-unset.json", `{"env":{"ANTHROPIC_BASE_URL":""}}`)
	if s := resolve(); s.Env[config.ClaudeBaseURLEnv] != "" {
		t.Fatal("explicit managed unset resurrected a shadowed endpoint")
	}
}

func TestClaudeConnectionIdentityTracksOnlyEffectiveNativeConnection(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, ".claude")
	write := func(path, body string) {
		t.Helper()
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	native := filepath.Join(dir, "settings.json")
	write(native, `{"env":{"ANTHROPIC_BASE_URL":"https://one.invalid","ANTHROPIC_AUTH_TOKEN":"fixture-one"}}`)
	env := []string{"HOME=" + home}
	identity := func(profile config.ClaudeProfile) string {
		t.Helper()
		v, e := teamClaudeConnectionIdentity(env, profile, cwd)
		if e != nil {
			t.Fatal(e)
		}
		if len(v) != 64 {
			t.Fatal("connection lacks opaque digest")
		}
		return v
	}
	built := config.BuiltInClaudeProfile()
	first := identity(built)
	write(native, `{"env":{"ANTHROPIC_BASE_URL":"https://two.invalid","ANTHROPIC_AUTH_TOKEN":"fixture-two"}}`)
	if identity(built) == first {
		t.Fatal("native provider/account switch retained old identity")
	}
	current := identity(built)
	write(native, `{"env":{"ANTHROPIC_BASE_URL":"https://two.invalid","ANTHROPIC_AUTH_TOKEN":"fixture-two","ANTHROPIC_MODEL":"other","AWS_REGION":"unused","CLAUDE_CODE_USE_VERTEX":"false"},"permissions":{"defaultMode":"plan"},"apiKeyHelper":"unused helper"}`)
	if identity(built) != current {
		t.Fatal("model/permission/unused provider/helper reset history")
	}
	custom := config.ClaudeProfile{ClaudeProfileConfig: config.ClaudeProfileConfig{ID: "api", BaseURL: "https://fixed.invalid", AuthMode: config.ClaudeAuthModeAuthToken, AuthToken: "fixture-fixed"}}
	fixed := identity(custom)
	write(native, `{"env":{"ANTHROPIC_BASE_URL":"https://shadowed.invalid","ANTHROPIC_AUTH_TOKEN":"shadowed"}}`)
	write(filepath.Join(home, ".claude.json"), `{"oauthAccount":{"accountUuid":"unused-account-b"}}`)
	write(filepath.Join(dir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"unused-rotated"}}`)
	if identity(custom) != fixed {
		t.Fatal("shadowed native provider/auth/OAuth refresh reset fixed API profile")
	}
	write(filepath.Join(cwd, ".claude", "settings.local.json"), `{"env":{"ANTHROPIC_BASE_URL":"local","ANTHROPIC_AUTH_TOKEN":"local-key"}}`)
	local := identity(built)
	write(native, `{"env":{"ANTHROPIC_BASE_URL":"different-unused","ANTHROPIC_AUTH_TOKEN":"unused"}}`)
	if identity(built) != local {
		t.Fatal("overridden lower settings reset local connection")
	}
}

func TestClaudeConnectionProviderIgnoresOtherProviderCredentials(t *testing.T) {
	env := map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1", "AWS_REGION": "region", "ANTHROPIC_API_KEY": "unused", "ANTHROPIC_BASE_URL": "unused", "CLOUD_ML_REGION": "unused"}
	selected := effectiveClaudeConnectionEnv(env)
	if selected["provider"] != "bedrock" || selected["AWS_REGION"] != "region" || selected["ANTHROPIC_API_KEY"] != "" || selected["CLOUD_ML_REGION"] != "" {
		t.Fatal("inactive provider credentials entered identity")
	}
}

func TestClaudeConnectionOwnedGitLocalOverridesLegacyCWDLocal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows retains CWD local settings")
	}
	home, repo := t.TempDir(), t.TempDir()
	cwd := filepath.Join(repo, "nested")
	dir := filepath.Join(home, ".claude")
	for _, path := range []string{filepath.Join(repo, ".git"), filepath.Join(repo, ".claude"), filepath.Join(cwd, ".claude"), dir} {
		if e := os.MkdirAll(path, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for path, body := range map[string]string{filepath.Join(cwd, ".claude", "settings.json"): `{"env":{"ANTHROPIC_BASE_URL":"cwd-shared"}}`, filepath.Join(cwd, ".claude", "settings.local.json"): `{"env":{"ANTHROPIC_AUTH_TOKEN":"legacy-local"}}`, filepath.Join(repo, ".claude", "settings.local.json"): `{"env":{"ANTHROPIC_AUTH_TOKEN":"root-local"}}`, filepath.Join(repo, ".claude", "settings.json"): `{"env":{"ANTHROPIC_BASE_URL":"unused-root-shared"}}`} {
		if e := os.WriteFile(path, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	result, e := effectiveClaudeConnectionSettings(map[string]string{"HOME": home}, config.BuiltInClaudeProfile(), dir, cwd, "")
	if e != nil {
		t.Fatal(e)
	}
	if result.Env[config.ClaudeBaseURLEnv] != "cwd-shared" || result.Env[config.ClaudeAuthTokenEnv] != "root-local" {
		t.Fatal("native CWD-shared/root-local precedence lost")
	}
}

func TestClaudeConnectionInvalidUnusedEntryDoesNotDiscardValidConnection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if e := os.WriteFile(path, []byte(`{"env":{"ANTHROPIC_BASE_URL":"valid","UNUSED":10},"apiKeyHelper":false}`), 0600); e != nil {
		t.Fatal(e)
	}
	result, e := effectiveClaudeConnectionSettings(nil, config.BuiltInClaudeProfile(), dir, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if result.Env[config.ClaudeBaseURLEnv] != "valid" {
		t.Fatal("invalid unused setting suppressed valid native connection")
	}
}

func TestClaudeConnectionCustomProfileClearsShellAndUsesActualRuntimeOverlay(t *testing.T) {
	dir := t.TempDir()
	values := map[string]string{config.ClaudeBaseURLEnv: "unused-shell-route", config.ClaudeAuthTokenEnv: "unused-shell-token"}
	custom := config.ClaudeProfile{ClaudeProfileConfig: config.ClaudeProfileConfig{ID: "inherit", AuthMode: config.ClaudeAuthModeInherit}}
	result, e := effectiveClaudeConnectionSettings(values, custom, dir, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if result.Env[config.ClaudeBaseURLEnv] != "" || result.Env[config.ClaudeAuthTokenEnv] != "" {
		t.Fatal("custom inherit profile resurrected shell values cleared by production launch")
	}
	values[config.ClaudeRuntimeSettingsJSONEnv] = `{"env":{"ANTHROPIC_BASE_URL":"inherited-overlay","ANTHROPIC_AUTH_TOKEN":"overlay-token"}}`
	built := config.BuiltInClaudeProfile()
	result, e = effectiveClaudeConnectionSettings(values, built, dir, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if result.Env[config.ClaudeBaseURLEnv] != "inherited-overlay" {
		t.Fatal("actual inherited --settings overlay ignored")
	}
	built.Model = "explicit-model"
	result, e = effectiveClaudeConnectionSettings(values, built, dir, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if result.Env[config.ClaudeBaseURLEnv] != "unused-shell-route" {
		t.Fatal("replaced internal overlay still influenced connection")
	}
}

func TestClaudeConnectionAuthenticationHeadersEnterOnlyOpaqueIdentity(t *testing.T) {
	home := t.TempDir()
	profile := config.ClaudeProfile{ClaudeProfileConfig: config.ClaudeProfileConfig{ID: "headers", BaseURL: "https://fixture.invalid", AuthMode: config.ClaudeAuthModeAuthToken, AuthToken: "fixed-token"}}
	identity := func(headers string) string {
		t.Helper()
		value, e := teamClaudeConnectionIdentity([]string{"HOME=" + home, "ANTHROPIC_CUSTOM_HEADERS=" + headers}, profile)
		if e != nil {
			t.Fatal(e)
		}
		if len(value) != 64 {
			t.Fatal("header connection did not produce opaque digest")
		}
		return value
	}
	native, e := teamClaudeConnectionIdentity([]string{"HOME=" + home, "ANTHROPIC_CUSTOM_HEADERS=Authorization: Bearer fixture-one"}, config.BuiltInClaudeProfile())
	if e != nil || len(native) != 64 {
		t.Fatal("header-only native authentication lacked connection evidence")
	}
	first := identity("Authorization: Bearer fixture-one\nx-api-key: fixture-key-one")
	if identity("Authorization: Bearer fixture-one\nx-api-key: fixture-key-one") != first {
		t.Fatal("unchanged native authentication headers reset identity")
	}
	if identity("Authorization: Bearer fixture-two\nx-api-key: fixture-key-one") == first {
		t.Fatal("changed Authorization header reused prior connection")
	}
	if identity("Authorization: Bearer fixture-one\nx-api-key: fixture-key-two") == first {
		t.Fatal("changed x-api-key header reused prior connection")
	}
}
