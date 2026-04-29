package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestAdminClaudeProfilesCRUDAndRedaction(t *testing.T) {
	app, configPath := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	createRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{
  "id":"devseek",
  "name":"DevSeek",
  "authMode":"auth_token",
  "baseURL":"https://proxy.internal/v1",
  "authToken":"secret-token",
  "model":"mimo-v2.5-pro",
  "smallModel":"mimo-v2.5-haiku"
}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "secret-token") {
		t.Fatalf("create response leaked auth token: %s", createRec.Body.String())
	}
	var createResp claudeProfileResponse
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResp.Profile.ID != "devseek" || !createResp.Profile.HasAuthToken || createResp.Profile.BuiltIn || createResp.Profile.ReadOnly {
		t.Fatalf("unexpected create response: %#v", createResp.Profile)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after create: %v", err)
	}
	if len(loaded.Config.Claude.Profiles) != 1 || loaded.Config.Claude.Profiles[0].AuthToken != "secret-token" {
		t.Fatalf("expected auth token to persist in config file, got %#v", loaded.Config.Claude.Profiles)
	}

	listRec := performAdminRequest(t, app, http.MethodGet, "/api/admin/claude/profiles", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp claudeProfilesResponse
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Profiles) != 2 {
		t.Fatalf("expected default + custom profile, got %#v", listResp.Profiles)
	}
	if listResp.Profiles[0].ID != config.ClaudeDefaultProfileID || !listResp.Profiles[0].BuiltIn || !listResp.Profiles[0].ReadOnly || listResp.Profiles[0].Persisted {
		t.Fatalf("unexpected built-in default profile view: %#v", listResp.Profiles[0])
	}
	if listResp.Profiles[1].ID != "devseek" || listResp.Profiles[1].BaseURL != "https://proxy.internal/v1" || !listResp.Profiles[1].Persisted {
		t.Fatalf("unexpected custom profile view: %#v", listResp.Profiles[1])
	}

	configRec := performAdminRequest(t, app, http.MethodGet, "/api/admin/config", "")
	if configRec.Code != http.StatusOK {
		t.Fatalf("config status = %d, want 200 body=%s", configRec.Code, configRec.Body.String())
	}
	if strings.Contains(configRec.Body.String(), "secret-token") {
		t.Fatalf("config response leaked auth token: %s", configRec.Body.String())
	}
	var configResp adminConfigResponse
	if err := json.NewDecoder(configRec.Body).Decode(&configResp); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if len(configResp.Config.Claude.Profiles) != 1 || !configResp.Config.Claude.Profiles[0].HasAuthToken {
		t.Fatalf("unexpected redacted claude config: %#v", configResp.Config.Claude)
	}

	updateRec := performAdminRequest(t, app, http.MethodPut, "/api/admin/claude/profiles/devseek", `{
  "name":"DevSeek 2",
  "authMode":"inherit",
  "baseURL":"",
  "model":"",
  "smallModel":"",
  "clearAuthToken":true
}`)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp claudeProfileResponse
	if err := json.NewDecoder(updateRec.Body).Decode(&updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResp.Profile.Name != "DevSeek 2" || updateResp.Profile.AuthMode != config.ClaudeAuthModeInherit || updateResp.Profile.HasAuthToken {
		t.Fatalf("unexpected update response: %#v", updateResp.Profile)
	}
	if updateResp.Profile.BaseURL != "" || updateResp.Profile.Model != "" || updateResp.Profile.SmallModel != "" {
		t.Fatalf("expected cleared override fields, got %#v", updateResp.Profile)
	}

	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after update: %v", err)
	}
	if len(loaded.Config.Claude.Profiles) != 1 {
		t.Fatalf("unexpected profile count after update: %#v", loaded.Config.Claude.Profiles)
	}
	if loaded.Config.Claude.Profiles[0].AuthToken != "" || loaded.Config.Claude.Profiles[0].BaseURL != "" {
		t.Fatalf("expected cleared auth/base settings in config file, got %#v", loaded.Config.Claude.Profiles[0])
	}

	readOnlyRec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/claude/profiles/default", "")
	if readOnlyRec.Code != http.StatusConflict {
		t.Fatalf("default delete status = %d, want 409 body=%s", readOnlyRec.Code, readOnlyRec.Body.String())
	}

	deleteRec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/claude/profiles/devseek", "")
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	listRec = performAdminRequest(t, app, http.MethodGet, "/api/admin/claude/profiles", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("final list status = %d, want 200 body=%s", listRec.Code, listRec.Body.String())
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode final list response: %v", err)
	}
	if len(listResp.Profiles) != 1 || listResp.Profiles[0].ID != config.ClaudeDefaultProfileID {
		t.Fatalf("expected only built-in default profile after delete, got %#v", listResp.Profiles)
	}
}
