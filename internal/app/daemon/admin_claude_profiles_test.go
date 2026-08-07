package daemon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestAdminClaudeContextPreferenceUsesIndependentItemETag(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	list := performAdminRequest(t, app, http.MethodGet, "/api/admin/claude/profiles", "")
	if list.Code != http.StatusOK {
		t.Fatalf("initial list status = %d body=%s", list.Code, list.Body.String())
	}
	var listed claudeProfilesResponse
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatalf("decode initial list: %v", err)
	}
	if len(listed.Profiles) != 1 || listed.Profiles[0].ContextPreference.Revision != 1 || listed.Profiles[0].ContextPreference.ETag == "" {
		t.Fatalf("default Claude preference missing: %#v", listed.Profiles)
	}
	preference := listed.Profiles[0].ContextPreference
	path := "/api/admin/claude/profiles/default/context-preference"
	missing := performAdminRequest(t, app, http.MethodPut, path, `{"mode":"extended_1m"}`)
	if missing.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing precondition status = %d body=%s", missing.Code, missing.Body.String())
	}
	assertAdminAPIErrorCode(t, missing, "profile_preference_revision_required")
	stale := performAdminRequestWithIfMatch(t, app, http.MethodPut, path, `{"mode":"extended_1m"}`, `"stale"`)
	if stale.Code != http.StatusPreconditionFailed || stale.Header().Get("ETag") != preference.ETag {
		t.Fatalf("stale precondition status = %d etag=%q body=%s", stale.Code, stale.Header().Get("ETag"), stale.Body.String())
	}
	staleError := assertAdminAPIErrorCode(t, stale, "profile_preference_revision_conflict")
	if staleError.Details == nil {
		t.Fatalf("stale preference omitted current state: %s", stale.Body.String())
	}
	updated := performAdminRequestWithIfMatch(t, app, http.MethodPut, path, `{"mode":"extended_1m"}`, preference.ETag)
	if updated.Code != http.StatusOK {
		t.Fatalf("context update status = %d body=%s", updated.Code, updated.Body.String())
	}
	var response claudeContextPreferenceResponse
	if err := json.NewDecoder(updated.Body).Decode(&response); err != nil {
		t.Fatalf("decode context update: %v", err)
	}
	if response.ContextPreference.Revision != 2 || response.ContextPreference.Mode != state.ClaudeContextModeExtended {
		t.Fatalf("unexpected context update: %#v", response.ContextPreference)
	}
	if response.ContextPreference.ETag == preference.ETag || updated.Header().Get("ETag") != response.ContextPreference.ETag {
		t.Fatalf("context ETag did not advance: old=%q response=%#v header=%q", preference.ETag, response, updated.Header().Get("ETag"))
	}

	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{"name":"DevSeek","model":"claude-sonnet-4-5"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var created claudeProfileResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Profile.ContextPreference.Revision != 1 || created.Profile.ContextPreference.ETag == "" {
		t.Fatalf("Claude write response omitted context preference: %#v", created.Profile)
	}
}

func TestAdminClaudeProfilesCRUDAndRedaction(t *testing.T) {
	app, configPath := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	createRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{
  "name":"DevSeek",
  "baseURL":"https://proxy.internal/v1",
  "authToken":"secret-token",
  "model":"mimo-v2.5-pro",
  "smallModel":"mimo-v2.5-haiku",
  "reasoningEffort":"HIGH"
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
	if createResp.Profile.ID != "devseek" || !createResp.Profile.HasAuthToken || createResp.Profile.ReasoningEffort != "high" || createResp.Profile.BuiltIn || createResp.Profile.ReadOnly {
		t.Fatalf("unexpected create response: %#v", createResp.Profile)
	}
	if got := app.service.ClaudeProfiles(); len(got) != 2 || got[1].ID != "devseek" || got[1].ReasoningEffort != "high" {
		t.Fatalf("expected runtime catalog to include default + devseek after create, got %#v", got)
	}

	retryRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{
  "name":"DevSeek",
  "baseURL":"https://proxy.retry/v1",
  "model":"mimo-v2.5-pro"
}`)
	if retryRec.Code != http.StatusCreated {
		t.Fatalf("retry create status = %d, want 201 body=%s", retryRec.Code, retryRec.Body.String())
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after retry create: %v", err)
	}
	if len(loaded.Config.Claude.Profiles) != 1 {
		t.Fatalf("expected idempotent same-name create to keep one profile, got %#v", loaded.Config.Claude.Profiles)
	}
	if loaded.Config.Claude.Profiles[0].AuthToken != "secret-token" || loaded.Config.Claude.Profiles[0].BaseURL != "https://proxy.retry/v1" || loaded.Config.Claude.Profiles[0].ReasoningEffort != "high" {
		t.Fatalf("expected retry create to update visible fields and keep token, got %#v", loaded.Config.Claude.Profiles)
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
	if listResp.Profiles[1].ID != "devseek" || listResp.Profiles[1].BaseURL != "https://proxy.retry/v1" || listResp.Profiles[1].ReasoningEffort != "high" || !listResp.Profiles[1].Persisted {
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
  "baseURL":"https://proxy.second/v1",
  "model":"",
  "smallModel":"",
  "reasoningEffort":""
}`)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp claudeProfileResponse
	if err := json.NewDecoder(updateRec.Body).Decode(&updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResp.Profile.ID != "devseek-2" || updateResp.Profile.Name != "DevSeek 2" || updateResp.Profile.AuthMode != config.ClaudeAuthModeAuthToken || !updateResp.Profile.HasAuthToken {
		t.Fatalf("unexpected update response: %#v", updateResp.Profile)
	}
	if updateResp.Profile.BaseURL != "https://proxy.second/v1" || updateResp.Profile.Model != "" || updateResp.Profile.SmallModel != "" || updateResp.Profile.ReasoningEffort != "" {
		t.Fatalf("expected cleared override fields, got %#v", updateResp.Profile)
	}
	if got := app.service.ClaudeProfiles(); len(got) != 2 || got[1].ID != "devseek-2" || got[1].Name != "DevSeek 2" || got[1].ReasoningEffort != "" {
		t.Fatalf("expected runtime catalog to reflect update, got %#v", got)
	}

	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after update: %v", err)
	}
	if len(loaded.Config.Claude.Profiles) != 1 {
		t.Fatalf("unexpected profile count after update: %#v", loaded.Config.Claude.Profiles)
	}
	if loaded.Config.Claude.Profiles[0].ID != "devseek-2" || loaded.Config.Claude.Profiles[0].AuthToken != "secret-token" || loaded.Config.Claude.Profiles[0].BaseURL != "https://proxy.second/v1" {
		t.Fatalf("expected renamed profile to keep token and update base settings, got %#v", loaded.Config.Claude.Profiles[0])
	}

	readOnlyRec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/claude/profiles/default", "")
	if readOnlyRec.Code != http.StatusConflict {
		t.Fatalf("default delete status = %d, want 409 body=%s", readOnlyRec.Code, readOnlyRec.Body.String())
	}

	deleteRec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/claude/profiles/devseek-2", "")
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
	if got := app.service.ClaudeProfiles(); len(got) != 1 || got[0].ID != config.ClaudeDefaultProfileID {
		t.Fatalf("expected runtime catalog to drop deleted profile, got %#v", got)
	}
}

func TestAdminClaudeProfilesSubagentModelRoundTrip(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{
  "name":"DevSeek",
  "baseURL":"https://proxy.internal/v1",
  "model":"mimo-v2.5-pro",
  "smallModel":"mimo-v2.5-haiku",
  "subagentModel":"mimo-v2.5-mini",
  "reasoningEffort":"high"
}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var created claudeProfileResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Profile.SubagentModel != "mimo-v2.5-mini" {
		t.Fatalf("created SubagentModel = %q, want %q", created.Profile.SubagentModel, "mimo-v2.5-mini")
	}

	update := performAdminRequestWithIfMatch(t, app, http.MethodPut, "/api/admin/claude/profiles/"+created.Profile.ID, `{
  "name":"DevSeek",
  "baseURL":"https://proxy.internal/v1",
  "model":"mimo-v2.5-pro",
  "smallModel":"mimo-v2.5-haiku",
  "subagentModel":"mimo-v2.5-mini-2",
  "reasoningEffort":"high"
}`, created.Profile.ContextPreference.ETag)
	if update.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", update.Code, update.Body.String())
	}
	var updated claudeProfileResponse
	if err := json.NewDecoder(update.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Profile.SubagentModel != "mimo-v2.5-mini-2" {
		t.Fatalf("updated SubagentModel = %q, want %q", updated.Profile.SubagentModel, "mimo-v2.5-mini-2")
	}

	list := performAdminRequest(t, app, http.MethodGet, "/api/admin/claude/profiles", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var listed claudeProfilesResponse
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, profile := range listed.Profiles {
		if profile.ID == created.Profile.ID && profile.SubagentModel != "mimo-v2.5-mini-2" {
			t.Fatalf("listed SubagentModel = %q, want %q", profile.SubagentModel, "mimo-v2.5-mini-2")
		}
	}
}

func TestAdminClaudeProfilesInstructionRoundTripAndLimit(t *testing.T) {
	app, configPath := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{
  "name":"DevSeek",
  "baseURL":"https://proxy.internal/v1",
  "model":"mimo-v2.5-pro",
  "instruction":"你是一个严谨的工程师。"
}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var created claudeProfileResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Profile.Instruction != "你是一个严谨的工程师。" {
		t.Fatalf("created Instruction = %q, want role prompt", created.Profile.Instruction)
	}

	list := performAdminRequest(t, app, http.MethodGet, "/api/admin/claude/profiles", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var listed claudeProfilesResponse
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, profile := range listed.Profiles {
		if profile.ID == created.Profile.ID {
			found = true
			if profile.Instruction != "你是一个严谨的工程师。" {
				t.Fatalf("listed Instruction = %q, want role prompt", profile.Instruction)
			}
		}
	}
	if !found {
		t.Fatalf("listed profiles missing created profile: %#v", listed.Profiles)
	}

	clear := performAdminRequest(t, app, http.MethodPut, "/api/admin/claude/profiles/"+created.Profile.ID, `{
  "name":"DevSeek",
  "baseURL":"https://proxy.internal/v1",
  "model":"mimo-v2.5-pro",
  "instruction":""
}`)
	if clear.Code != http.StatusOK {
		t.Fatalf("clear status = %d body=%s", clear.Code, clear.Body.String())
	}
	var cleared claudeProfileResponse
	if err := json.NewDecoder(clear.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode clear: %v", err)
	}
	if cleared.Profile.Instruction != "" {
		t.Fatalf("cleared Instruction = %q, want empty", cleared.Profile.Instruction)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Claude.Profiles) != 1 || loaded.Config.Claude.Profiles[0].Instruction != "" {
		t.Fatalf("persisted profiles = %#v, want cleared instruction", loaded.Config.Claude.Profiles)
	}

	oversized := strings.Repeat("a", config.InstructionMaxChars+1)
	rejected := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{
  "name":"Too Long",
  "baseURL":"https://proxy.internal/v1",
  "instruction":`+strconv.Quote(oversized)+`
}`)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("oversized create status = %d body=%s", rejected.Code, rejected.Body.String())
	}
	assertAdminAPIErrorCode(t, rejected, "claude_profile_instruction_invalid")
}

func TestAdminClaudeProfileNameRequired(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	createRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{
  "name":" ",
  "baseURL":"https://proxy.internal/v1"
}`)
	if createRec.Code != http.StatusBadRequest {
		t.Fatalf("create empty name status = %d, want 400 body=%s", createRec.Code, createRec.Body.String())
	}

	updateRec := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{
  "name":"中文配置",
  "baseURL":"https://proxy.internal/v1"
}`)
	if updateRec.Code != http.StatusCreated {
		t.Fatalf("create unicode name status = %d, want 201 body=%s", updateRec.Code, updateRec.Body.String())
	}
	var resp claudeProfileResponse
	if err := json.NewDecoder(updateRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode unicode create response: %v", err)
	}
	if !strings.HasPrefix(resp.Profile.ID, "profile-") || resp.Profile.Name != "中文配置" {
		t.Fatalf("expected deterministic fallback id for unicode name, got %#v", resp.Profile)
	}

	emptyUpdateRec := performAdminRequest(t, app, http.MethodPut, "/api/admin/claude/profiles/"+resp.Profile.ID, `{
  "name":" "
}`)
	if emptyUpdateRec.Code != http.StatusBadRequest {
		t.Fatalf("update empty name status = %d, want 400 body=%s", emptyUpdateRec.Code, emptyUpdateRec.Body.String())
	}
}
