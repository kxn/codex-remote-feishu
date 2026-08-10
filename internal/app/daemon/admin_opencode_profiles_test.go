package daemon

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestAdminOpenCodeProfilesCRUDRevisionAndRedaction(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	binaryPath := filepath.Join(home, executableName("codex-remote"))
	writeExecutableFile(t, binaryPath, "wrapper-binary")
	app, configPath, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/opencode/profiles", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var listPayload struct {
		Profiles []adminOpenCodeProfileView `json:"profiles"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listPayload.Profiles) != 1 || !listPayload.Profiles[0].BuiltIn || listPayload.Profiles[0].ID != config.OpenCodeDefaultProfileID {
		t.Fatalf("expected built-in OpenCode default profile, got %#v", listPayload.Profiles)
	}

	createBody := `{"name":"Team OpenCode","baseURL":"https://proxy.example/v1","apiKey":"secret-v1","model":"kimi-k2","smallModel":"kimi-small","projectConfigMode":"disable"}`
	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/opencode/profiles", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	createETag := rec.Header().Get("ETag")
	if createETag == "" {
		t.Fatal("expected create response ETag")
	}
	var createPayload struct {
		Profile adminOpenCodeProfileView `json:"profile"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	created := createPayload.Profile
	if !strings.HasPrefix(created.ID, "op_") || created.Name != "Team OpenCode" || !created.HasAPIKey || created.APIKey != "" {
		t.Fatalf("unexpected created profile view: %#v", created)
	}
	if strings.Contains(rec.Body.String(), "secret-v1") {
		t.Fatalf("create response leaked secret: %s", rec.Body.String())
	}
	if profile, ok := findOpenCodeProfileSummary(app.service.OpenCodeProfiles(), created.ID); !ok ||
		profile.Revision != 1 || profile.Name != "Team OpenCode" || profile.Model != "kimi-k2" || !profile.Available {
		t.Fatalf("created profile was not materialized into orchestrator catalog: %#v ok=%t", profile, ok)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	current, ok := config.CurrentOpenCodeAPIProfile(loaded.Config.OpenCode.Profiles[0])
	if !ok || current.APIKey != "secret-v1" || current.Revision != 1 {
		t.Fatalf("expected secret persisted in config only, got %#v ok=%t", current, ok)
	}

	updateBody := `{"name":"Team OpenCode","baseURL":"https://proxy.example/v1","model":"kimi-k2-pro","smallModel":"kimi-small","projectConfigMode":"disable"}`
	rec = performAdminRequestWithIfMatch(t, app, http.MethodPut, "/api/admin/opencode/profiles/"+created.ID, updateBody, createETag)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var updatePayload struct {
		Profile adminOpenCodeProfileView `json:"profile"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&updatePayload); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updatePayload.Profile.Revision != 2 || updatePayload.Profile.Model != "kimi-k2-pro" || !updatePayload.Profile.HasAPIKey {
		t.Fatalf("unexpected updated profile view: %#v", updatePayload.Profile)
	}
	if profile, ok := findOpenCodeProfileSummary(app.service.OpenCodeProfiles(), created.ID); !ok ||
		profile.Revision != 2 || profile.Name != "Team OpenCode" || profile.Model != "kimi-k2-pro" || !profile.Available {
		t.Fatalf("updated profile was not materialized into orchestrator catalog: %#v ok=%t", profile, ok)
	}
	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after update: %v", err)
	}
	current, ok = config.CurrentOpenCodeAPIProfile(loaded.Config.OpenCode.Profiles[0])
	if !ok || current.APIKey != "secret-v1" || current.Revision != 2 {
		t.Fatalf("expected update to preserve secret when apiKey omitted, got %#v ok=%t", current, ok)
	}
}

func TestAdminOpenCodeProfileUpdatePreservesOmittedHiddenFields(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	binaryPath := filepath.Join(home, executableName("codex-remote"))
	writeExecutableFile(t, binaryPath, "wrapper-binary")
	app, configPath, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)

	createBody := `{"name":"Team OpenCode","baseURL":"https://proxy.example/v1","apiKey":"secret-v1","model":"kimi-k2","smallModel":"kimi-small","reviewModel":"kimi-review","subagentModel":"kimi-subagent","instruction":"be precise","reasoningEffort":"high","projectConfigMode":"disable","dataIsolationMode":"process","permissionMode":"ask"}`
	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/opencode/profiles", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	createETag := rec.Header().Get("ETag")
	var createPayload struct {
		Profile adminOpenCodeProfileView `json:"profile"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	updateBody := `{"name":"Team OpenCode","baseURL":"https://proxy.example/v1","model":"kimi-k2-pro","smallModel":"kimi-small-2","subagentModel":"kimi-subagent-2","instruction":"be exact","reasoningEffort":"medium"}`
	rec = performAdminRequestWithIfMatch(t, app, http.MethodPut, "/api/admin/opencode/profiles/"+createPayload.Profile.ID, updateBody, createETag)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after update: %v", err)
	}
	current, ok := config.CurrentOpenCodeAPIProfile(loaded.Config.OpenCode.Profiles[0])
	if !ok {
		t.Fatal("CurrentOpenCodeAPIProfile() did not return updated profile")
	}
	if current.APIKey != "secret-v1" {
		t.Fatalf("APIKey = %q, want preserved secret", current.APIKey)
	}
	if current.ReviewModel != "kimi-review" {
		t.Fatalf("ReviewModel = %q, want preserved hidden review model", current.ReviewModel)
	}
	if current.ProjectConfigMode != config.OpenCodeProjectConfigDisable {
		t.Fatalf("ProjectConfigMode = %q, want preserved disable", current.ProjectConfigMode)
	}
	if current.DataIsolationMode != config.OpenCodeDataIsolationProcess {
		t.Fatalf("DataIsolationMode = %q, want preserved process", current.DataIsolationMode)
	}
	if current.PermissionMode != "ask" {
		t.Fatalf("PermissionMode = %q, want preserved ask", current.PermissionMode)
	}
	if current.Model != "kimi-k2-pro" || current.SmallModel != "kimi-small-2" ||
		current.SubagentModel != "kimi-subagent-2" || current.Instruction != "be exact" ||
		current.ReasoningEffort != "medium" {
		t.Fatalf("visible fields were not updated: %#v", current)
	}
}

func findOpenCodeProfileSummary(profiles []state.OpenCodeProfileSummary, profileID string) (state.OpenCodeProfileSummary, bool) {
	profileID = state.NormalizeOpenCodeProfileID(profileID)
	for _, profile := range profiles {
		if state.NormalizeOpenCodeProfileID(profile.ID) == profileID {
			return profile, true
		}
	}
	return state.OpenCodeProfileSummary{}, false
}

func TestAdminOpenCodeProfileReferencesBlockDelete(t *testing.T) {
	record, err := config.PrepareOpenCodeAPIProfileCreate(nil, config.OpenCodeAPIProfileInput{
		Name: "Team OpenCode", BaseURL: "https://proxy.example/v1", APIKey: "secret", Model: "kimi-k2",
	})
	if err != nil {
		t.Fatalf("PrepareOpenCodeAPIProfileCreate: %v", err)
	}
	current, ok := config.CurrentOpenCodeAPIProfile(record)
	if !ok {
		t.Fatal("CurrentOpenCodeAPIProfile() did not return current profile")
	}

	home := t.TempDir()
	setTestHome(t, home)
	binaryPath := filepath.Join(home, executableName("codex-remote"))
	writeExecutableFile(t, binaryPath, "wrapper-binary")
	app, configPath, _ := newVSCodeAdminTestApp(t, home, binaryPath, false)
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	loaded.Config.OpenCode.Profiles = []config.OpenCodeAPIProfileRecord{record}
	if err := config.WriteAppConfig(configPath, loaded.Config); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app.service.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendOpenCode, "", "", "")
	surface := app.service.Surface("surface-1")
	surface.OpenCodeProfileID = current.ID
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: current.ID, Revision: current.Revision}}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:           "inst-opencode",
		Backend:              agentproto.BackendOpenCode,
		OpenCodeProfileID:    current.ID,
		OpenCodeAdmissionRef: surface.OpenCodeAdmissionRef,
		Threads:              map[string]*state.ThreadRecord{},
	})

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/opencode/profiles/"+current.ID+"/references", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("references status = %d body=%s", rec.Code, rec.Body.String())
	}
	var refsPayload opencodeProfileReferencesResponse
	if err := json.NewDecoder(rec.Body).Decode(&refsPayload); err != nil {
		t.Fatalf("decode references: %v", err)
	}
	if len(refsPayload.References) < 2 {
		t.Fatalf("expected surface and instance references, got %#v", refsPayload)
	}

	rec = performAdminRequestWithIfMatch(t, app, http.MethodDelete, "/api/admin/opencode/profiles/"+current.ID, "", state.OpenCodeProfileDefinitionETag(current.ID, current.Revision))
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete status = %d, want conflict body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "profile_in_use") {
		t.Fatalf("delete conflict body missing profile_in_use: %s", rec.Body.String())
	}
}
