package daemon

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nativeCatalogFixtureJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "fixture." + base64.RawURLEncoding.EncodeToString(raw) + ".unsigned-fixture"
}
func writeNativeCatalogAuth(t *testing.T, home string, body any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, "auth.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func nativeCatalogFingerprints(t *testing.T, env, keys []string) (string, string) {
	t.Helper()
	identity, err := nativeCatalogCredentialIdentity(env, keys)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := nativeCatalogCredentialGuard(env, keys)
	if err != nil {
		t.Fatal(err)
	}
	return identity, guard
}

func TestNativeCatalogOAuthTokenRefreshKeepsIdentityButChangesGuard(t *testing.T) {
	home := t.TempDir()
	env := []string{"CODEX_HOME=" + home}
	claims := map[string]any{"sub": "actual-subject", "email": "actual@example.invalid", "exp": 1, "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "actual-account", "chatgpt_plan_type": "plus"}}
	auth := map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{"account_id": "actual-account", "access_token": "fixture-access-one", "refresh_token": "fixture-refresh-one", "id_token": nativeCatalogFixtureJWT(t, claims)}, "last_refresh": "before"}
	writeNativeCatalogAuth(t, home, auth)
	before, guard := nativeCatalogFingerprints(t, env, nil)
	claims["exp"] = 2
	claims["iat"] = 2
	tokens := auth["tokens"].(map[string]any)
	tokens["access_token"] = "fixture-access-two"
	tokens["refresh_token"] = "fixture-refresh-two"
	tokens["id_token"] = nativeCatalogFixtureJWT(t, claims)
	auth["last_refresh"] = "after"
	writeNativeCatalogAuth(t, home, auth)
	after, nextGuard := nativeCatalogFingerprints(t, env, nil)
	if before != after {
		t.Fatal("OAuth token renewal discarded the account catalog namespace")
	}
	if guard == nextGuard {
		t.Fatal("raw credential guard missed token rotation during probe")
	}
	for _, secret := range []string{"actual-account", "actual-subject", "actual@example.invalid", "fixture-access-two", "fixture-refresh-two"} {
		if strings.Contains(after, secret) || strings.Contains(nextGuard, secret) {
			t.Fatal("credential data escaped its digest")
		}
	}
	tokens["account_id"] = "different-account"
	writeNativeCatalogAuth(t, home, auth)
	changed, _ := nativeCatalogFingerprints(t, env, nil)
	if changed == after {
		t.Fatal("different native account borrowed old account cache")
	}
	tokens["account_id"] = "actual-account"
	claims["https://api.openai.com/auth"].(map[string]any)["chatgpt_plan_type"] = "pro"
	tokens["id_token"] = nativeCatalogFixtureJWT(t, claims)
	writeNativeCatalogAuth(t, home, auth)
	changed, _ = nativeCatalogFingerprints(t, env, nil)
	if changed == after {
		t.Fatal("changed account plan borrowed old entitlement namespace")
	}
}

func TestNativeCatalogIdentityUsesStableClaimsAndConservativeFallback(t *testing.T) {
	for _, field := range []string{"sub", "email", "chatgpt_account_id", "tokens.account_id"} {
		t.Run(field, func(t *testing.T) {
			home := t.TempDir()
			env := []string{"CODEX_HOME=" + home}
			claims := map[string]any{"exp": 1}
			tokens := map[string]any{"access_token": "first"}
			if field == "tokens.account_id" {
				tokens["account_id"] = "actual-one"
			} else {
				claims[field] = "actual-one"
				tokens["id_token"] = nativeCatalogFixtureJWT(t, claims)
			}
			auth := map[string]any{"auth_mode": "chatgpt", "tokens": tokens}
			writeNativeCatalogAuth(t, home, auth)
			before, guard := nativeCatalogFingerprints(t, env, nil)
			tokens["access_token"] = "second"
			claims["exp"] = 2
			if field != "tokens.account_id" {
				tokens["id_token"] = nativeCatalogFixtureJWT(t, claims)
			}
			writeNativeCatalogAuth(t, home, auth)
			after, nextGuard := nativeCatalogFingerprints(t, env, nil)
			if before != after || guard == nextGuard {
				t.Fatal("stable native account claim was tied to expiring token")
			}
			if field == "tokens.account_id" {
				tokens["account_id"] = "actual-two"
			} else {
				claims[field] = "actual-two"
				tokens["id_token"] = nativeCatalogFixtureJWT(t, claims)
			}
			writeNativeCatalogAuth(t, home, auth)
			changed, _ := nativeCatalogFingerprints(t, env, nil)
			if changed == after {
				t.Fatal("actual account claim change was ignored")
			}
		})
	}
	home := t.TempDir()
	env := []string{"CODEX_HOME=" + home}
	for _, raw := range []string{`{"auth_mode":"chatgpt","tokens":{"access_token":"opaque-a"}}`, `{"auth_mode":"chatgpt","tokens":{"access_token":"opaque-b"}}`, `not-json-fixture`} {
		if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		identity, guard := nativeCatalogFingerprints(t, env, nil)
		if identity == "" || guard == "" {
			t.Fatal("unknown native authentication lost its conservative digest")
		}
	}
	writeNativeCatalogAuth(t, home, map[string]any{"tokens": map[string]any{"access_token": "opaque-one"}})
	first, _ := nativeCatalogFingerprints(t, env, nil)
	writeNativeCatalogAuth(t, home, map[string]any{"tokens": map[string]any{"access_token": "opaque-two"}})
	second, _ := nativeCatalogFingerprints(t, env, nil)
	if first == second {
		t.Fatal("unknown account credentials incorrectly reused a cache namespace")
	}
}

func TestNativeCatalogCredentialEnvironmentAndAPIKeyIsolation(t *testing.T) {
	home := t.TempDir()
	writeNativeCatalogAuth(t, home, map[string]any{"auth_mode": "apikey", "OPENAI_API_KEY": "file-key-one"})
	env := []string{"CODEX_HOME=" + home, "OPENAI_API_KEY=env-key", "CUSTOM_PROVIDER_KEY=provider-key"}
	before, guard := nativeCatalogFingerprints(t, env, []string{"CUSTOM_PROVIDER_KEY", "OPENAI_API_KEY"})
	reordered, reorderedGuard := nativeCatalogFingerprints(t, []string{env[2], env[0], env[1], "UNRELATED=ignored"}, []string{"OPENAI_API_KEY", "CUSTOM_PROVIDER_KEY", "CUSTOM_PROVIDER_KEY"})
	if reordered != before || reorderedGuard != guard {
		t.Fatal("provider key or environment order changed stable semantic namespace")
	}
	for _, key := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "CUSTOM_PROVIDER_KEY"} {
		changed, nextGuard := nativeCatalogFingerprints(t, append(append([]string{}, env...), key+"=rotated-fixture"), []string{"CUSTOM_PROVIDER_KEY"})
		if changed == before || nextGuard == guard {
			t.Fatalf("%s credential rotation was not isolated", key)
		}
	}
	writeNativeCatalogAuth(t, home, map[string]any{"auth_mode": "apikey", "OPENAI_API_KEY": "file-key-two"})
	changed, _ := nativeCatalogFingerprints(t, env, []string{"CUSTOM_PROVIDER_KEY"})
	if changed == before {
		t.Fatal("native file API key change borrowed old cache")
	}
}

func TestNativeCatalogCredentialReadFailureDoesNotExposeSecrets(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "auth.json"), 0700); err != nil {
		t.Fatal(err)
	}
	env := []string{"CODEX_HOME=" + home, "OPENAI_API_KEY=fixture-never-print"}
	if identity, err := nativeCatalogCredentialIdentity(env, nil); err == nil || identity != "" || strings.Contains(err.Error(), home) || strings.Contains(err.Error(), "fixture-never-print") {
		t.Fatal("credential identity did not fail closed with sanitized error")
	}
	if guard, err := nativeCatalogCredentialGuard(env, nil); err == nil || guard != "" || strings.Contains(err.Error(), home) || strings.Contains(err.Error(), "fixture-never-print") {
		t.Fatal("credential guard did not fail closed with sanitized error")
	}
}
