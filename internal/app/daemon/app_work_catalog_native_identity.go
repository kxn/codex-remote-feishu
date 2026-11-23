package daemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
)

type nativeCatalogAuth struct {
	Mode   string `json:"auth_mode"`
	APIKey string `json:"OPENAI_API_KEY"`
	Tokens struct {
		AccountID string `json:"account_id"`
		IDToken   string `json:"id_token"`
	} `json:"tokens"`
}

type nativeCatalogAccount struct {
	Subject   string
	Email     string
	AccountID string
	Plan      string
}

// Stable account facts identify cached directory data; expiring OAuth tokens
// do not. Unrecognised credentials conservatively retain their raw digest.
func nativeCatalogCredentialIdentity(env, providerKeys []string) (string, error) {
	raw, credentials, err := nativeCatalogCredentialParts(env, providerKeys)
	if err != nil {
		return "", err
	}
	var auth nativeCatalogAuth
	if json.Unmarshal(raw, &auth) == nil {
		mode := strings.ToLower(strings.TrimSpace(auth.Mode))
		account := nativeCatalogIDTokenAccount(auth.Tokens.IDToken)
		accountID := strings.TrimSpace(auth.Tokens.AccountID)
		oauth := mode == "" || mode == "chatgpt" || mode == "chatgptauthtokens"
		if oauth && (accountID != "" || account.Subject != "" || account.Email != "" || account.AccountID != "") {
			return workui.ID("native-catalog-account-v1", mode, auth.APIKey, accountID, account.Subject, account.Email, account.AccountID, account.Plan, credentials), nil
		}
		if auth.APIKey != "" && (mode == "" || mode == "apikey" || mode == "api_key") {
			return workui.ID("native-catalog-api-key-v1", mode, auth.APIKey, credentials), nil
		}
	}
	return workui.ID("native-catalog-opaque-auth-v1", string(raw), credentials), nil
}

// The stricter guard fences a live query across token/file or environment
// changes. It is never persisted as account identity or exposed to the user.
func nativeCatalogCredentialGuard(env, providerKeys []string) (string, error) {
	raw, credentials, err := nativeCatalogCredentialParts(env, providerKeys)
	if err != nil {
		return "", err
	}
	return workui.ID("native-catalog-credential-guard-v1", string(raw), credentials), nil
}

func nativeCatalogCredentialParts(env, providerKeys []string) ([]byte, string, error) {
	values := map[string]string{}
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok {
			values[key] = value
		}
	}
	home := values["CODEX_HOME"]
	if home == "" {
		userHome := values["HOME"]
		if userHome == "" {
			var err error
			userHome, err = os.UserHomeDir()
			if err != nil {
				return nil, "", fmt.Errorf("无法定位当前原生登录信息")
			}
		}
		home = filepath.Join(userHome, ".codex")
	}
	raw, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("无法读取当前原生登录信息")
	}
	keys := map[string]bool{"OPENAI_API_KEY": true, "CODEX_API_KEY": true, "CODEX_ACCESS_TOKEN": true}
	for _, key := range providerKeys {
		if key != "" {
			keys[key] = true
		}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	parts := []string{"native-catalog-credential-env-v1"}
	for _, key := range ordered {
		parts = append(parts, key, values[key])
	}
	return raw, workui.ID(parts...), nil
}

func nativeCatalogIDTokenAccount(token string) nativeCatalogAccount {
	var result nativeCatalogAccount
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return result
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return result
	}
	var claims map[string]any
	if json.Unmarshal(raw, &claims) != nil {
		return result
	}
	text := func(object map[string]any, key string) string {
		value, _ := object[key].(string)
		return strings.TrimSpace(value)
	}
	result.Subject = text(claims, "sub")
	result.Email = text(claims, "email")
	result.AccountID = text(claims, "chatgpt_account_id")
	result.Plan = text(claims, "chatgpt_plan_type")
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if result.AccountID == "" {
			result.AccountID = text(auth, "chatgpt_account_id")
		}
		if result.Plan == "" {
			result.Plan = text(auth, "chatgpt_plan_type")
		}
	}
	return result
}
