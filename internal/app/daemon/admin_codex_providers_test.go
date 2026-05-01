package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestAdminCodexProvidersCRUDAndRedaction(t *testing.T) {
	app, configPath := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	createRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":"Team Proxy",
  "baseURL":"https://proxy.internal/v1",
  "apiKey":"secret-key"
}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "secret-key") {
		t.Fatalf("create response leaked api key: %s", createRec.Body.String())
	}
	var createResp codexProviderResponse
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResp.Provider.ID != "team-proxy" || !createResp.Provider.HasAPIKey || createResp.Provider.BuiltIn || createResp.Provider.ReadOnly {
		t.Fatalf("unexpected create response: %#v", createResp.Provider)
	}
	if got := app.service.CodexProviders(); len(got) != 2 || got[1].ID != "team-proxy" {
		t.Fatalf("expected runtime catalog to include default + team-proxy after create, got %#v", got)
	}

	retryRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":"Team Proxy",
  "baseURL":"https://proxy.retry/v1"
}`)
	if retryRec.Code != http.StatusCreated {
		t.Fatalf("retry create status = %d, want 201 body=%s", retryRec.Code, retryRec.Body.String())
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after retry create: %v", err)
	}
	if len(loaded.Config.Codex.Providers) != 1 {
		t.Fatalf("expected idempotent same-name create to keep one provider, got %#v", loaded.Config.Codex.Providers)
	}
	if loaded.Config.Codex.Providers[0].APIKey != "secret-key" || loaded.Config.Codex.Providers[0].BaseURL != "https://proxy.retry/v1" {
		t.Fatalf("expected retry create to update visible fields and keep key, got %#v", loaded.Config.Codex.Providers)
	}

	listRec := performAdminRequest(t, app, http.MethodGet, "/api/admin/codex/providers", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp codexProvidersResponse
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Providers) != 2 {
		t.Fatalf("expected default + custom provider, got %#v", listResp.Providers)
	}
	if listResp.Providers[0].ID != config.CodexDefaultProviderID || !listResp.Providers[0].BuiltIn || !listResp.Providers[0].ReadOnly || listResp.Providers[0].Persisted {
		t.Fatalf("unexpected built-in default provider view: %#v", listResp.Providers[0])
	}
	if listResp.Providers[1].ID != "team-proxy" || listResp.Providers[1].BaseURL != "https://proxy.retry/v1" || !listResp.Providers[1].Persisted {
		t.Fatalf("unexpected custom provider view: %#v", listResp.Providers[1])
	}

	configRec := performAdminRequest(t, app, http.MethodGet, "/api/admin/config", "")
	if configRec.Code != http.StatusOK {
		t.Fatalf("config status = %d, want 200 body=%s", configRec.Code, configRec.Body.String())
	}
	if strings.Contains(configRec.Body.String(), "secret-key") {
		t.Fatalf("config response leaked api key: %s", configRec.Body.String())
	}
	var configResp adminConfigResponse
	if err := json.NewDecoder(configRec.Body).Decode(&configResp); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if len(configResp.Config.Codex.Providers) != 1 || !configResp.Config.Codex.Providers[0].HasAPIKey {
		t.Fatalf("unexpected redacted codex config: %#v", configResp.Config.Codex)
	}

	updateRec := performAdminRequest(t, app, http.MethodPut, "/api/admin/codex/providers/team-proxy", `{
  "name":"Team Proxy 2",
  "baseURL":"https://proxy.second/v1"
}`)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp codexProviderResponse
	if err := json.NewDecoder(updateRec.Body).Decode(&updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResp.Provider.ID != "team-proxy-2" || updateResp.Provider.Name != "Team Proxy 2" || !updateResp.Provider.HasAPIKey {
		t.Fatalf("unexpected update response: %#v", updateResp.Provider)
	}
	if updateResp.Provider.BaseURL != "https://proxy.second/v1" {
		t.Fatalf("expected updated base url, got %#v", updateResp.Provider)
	}
	if got := app.service.CodexProviders(); len(got) != 2 || got[1].ID != "team-proxy-2" || got[1].Name != "Team Proxy 2" {
		t.Fatalf("expected runtime catalog to reflect update, got %#v", got)
	}

	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after update: %v", err)
	}
	if len(loaded.Config.Codex.Providers) != 1 {
		t.Fatalf("unexpected provider count after update: %#v", loaded.Config.Codex.Providers)
	}
	if loaded.Config.Codex.Providers[0].ID != "team-proxy-2" || loaded.Config.Codex.Providers[0].APIKey != "secret-key" || loaded.Config.Codex.Providers[0].BaseURL != "https://proxy.second/v1" {
		t.Fatalf("expected renamed provider to keep key and update base settings, got %#v", loaded.Config.Codex.Providers[0])
	}

	readOnlyRec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/codex/providers/default", "")
	if readOnlyRec.Code != http.StatusConflict {
		t.Fatalf("default delete status = %d, want 409 body=%s", readOnlyRec.Code, readOnlyRec.Body.String())
	}

	deleteRec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/codex/providers/team-proxy-2", "")
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	listRec = performAdminRequest(t, app, http.MethodGet, "/api/admin/codex/providers", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("final list status = %d, want 200 body=%s", listRec.Code, listRec.Body.String())
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode final list response: %v", err)
	}
	if len(listResp.Providers) != 1 || listResp.Providers[0].ID != config.CodexDefaultProviderID {
		t.Fatalf("expected only built-in default provider after delete, got %#v", listResp.Providers)
	}
	if got := app.service.CodexProviders(); len(got) != 1 || got[0].ID != config.CodexDefaultProviderID {
		t.Fatalf("expected runtime catalog to drop deleted provider, got %#v", got)
	}
}

func TestAdminCodexProviderValidationAndReservedNames(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	createRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":" ",
  "baseURL":"https://proxy.internal/v1",
  "apiKey":"secret-key"
}`)
	if createRec.Code != http.StatusBadRequest {
		t.Fatalf("create empty name status = %d, want 400 body=%s", createRec.Code, createRec.Body.String())
	}

	missingKeyRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":"Proxy",
  "baseURL":"https://proxy.internal/v1"
}`)
	if missingKeyRec.Code != http.StatusBadRequest {
		t.Fatalf("create missing key status = %d, want 400 body=%s", missingKeyRec.Code, missingKeyRec.Body.String())
	}

	reservedNameRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":"OpenAI",
  "baseURL":"https://proxy.internal/v1",
  "apiKey":"secret-key"
}`)
	if reservedNameRec.Code != http.StatusConflict {
		t.Fatalf("reserved name status = %d, want 409 body=%s", reservedNameRec.Code, reservedNameRec.Body.String())
	}

	unicodeRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":"中文代理",
  "baseURL":"https://proxy.internal/v1",
  "apiKey":"secret-key"
}`)
	if unicodeRec.Code != http.StatusCreated {
		t.Fatalf("create unicode name status = %d, want 201 body=%s", unicodeRec.Code, unicodeRec.Body.String())
	}
	var resp codexProviderResponse
	if err := json.NewDecoder(unicodeRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode unicode create response: %v", err)
	}
	if !strings.HasPrefix(resp.Provider.ID, "provider-") || resp.Provider.Name != "中文代理" {
		t.Fatalf("expected deterministic fallback id for unicode name, got %#v", resp.Provider)
	}

	emptyUpdateRec := performAdminRequest(t, app, http.MethodPut, "/api/admin/codex/providers/"+resp.Provider.ID, `{
  "name":" ",
  "baseURL":"https://proxy.second/v1"
}`)
	if emptyUpdateRec.Code != http.StatusBadRequest {
		t.Fatalf("update empty name status = %d, want 400 body=%s", emptyUpdateRec.Code, emptyUpdateRec.Body.String())
	}
}
