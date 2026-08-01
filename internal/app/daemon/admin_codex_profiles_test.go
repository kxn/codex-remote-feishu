package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestAdminCodexProfilesCanonicalCRUDUsesItemETagsAndRedaction(t *testing.T) {
	app, configPath := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	initial := performAdminRequest(t, app, http.MethodGet, "/api/admin/codex/profiles", "")
	if initial.Code != http.StatusOK {
		t.Fatalf("initial list status = %d body=%s", initial.Code, initial.Body.String())
	}
	var initialResponse codexProfilesResponse
	if err := json.NewDecoder(initial.Body).Decode(&initialResponse); err != nil {
		t.Fatalf("decode initial list: %v", err)
	}
	if len(initialResponse.Profiles) != 1 || initialResponse.Profiles[0].ID != config.CodexNativeProfileID {
		t.Fatalf("initial profiles = %#v", initialResponse.Profiles)
	}
	native := initialResponse.Profiles[0]
	if native.Kind != state.CodexProfileKindNative || native.Editable || native.Deletable || !native.ContextEditable || native.ETag != "" {
		t.Fatalf("unexpected native summary: %#v", native)
	}
	if native.ContextPreference.ETag == "" || native.ContextPreference.Revision != 1 {
		t.Fatalf("native context preference missing revision/etag: %#v", native.ContextPreference)
	}

	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/profiles", `{
  "name":"Team Proxy",
  "baseURL":"https://proxy.example/v1",
  "apiKey":" secret with spaces ",
  "model":"gpt-5.4",
  "reviewModel":"gpt-5.4-mini",
  "reasoningEffort":"custom-effort"
}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	if strings.Contains(create.Body.String(), "secret with spaces") {
		t.Fatalf("create response leaked API key: %s", create.Body.String())
	}
	var createResponse codexProfileResponse
	if err := json.NewDecoder(create.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	created := createResponse.Profile
	if !strings.HasPrefix(created.ID, "cp_") || created.Kind != state.CodexProfileKindAPI || !created.HasAPIKey || !created.Editable {
		t.Fatalf("unexpected created profile: %#v", created)
	}
	if created.Revision != 1 || created.ETag == "" || create.Header().Get("ETag") != created.ETag {
		t.Fatalf("created profile revision/etag mismatch: %#v header=%q", created, create.Header().Get("ETag"))
	}
	if created.ContextPreference.Revision != 1 || created.ContextPreference.ETag == "" {
		t.Fatalf("created preference = %#v", created.ContextPreference)
	}
	references := performAdminRequest(t, app, http.MethodGet, "/api/admin/codex/profiles/"+created.ID+"/references", "")
	if references.Code != http.StatusOK || strings.Contains(references.Body.String(), "secret with spaces") {
		t.Fatalf("references response status=%d body=%s", references.Code, references.Body.String())
	}
	var referencesResponse codexProfileReferencesResponse
	if err := json.NewDecoder(references.Body).Decode(&referencesResponse); err != nil {
		t.Fatalf("decode references: %v", err)
	}
	if referencesResponse.ProfileID != created.ID || len(referencesResponse.References) != 0 {
		t.Fatalf("unexpected initial references: %#v", referencesResponse)
	}
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.Surface("surface-1").PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:      "inst-headless",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: created.ID,
		CodexAdmissionRef: &state.CodexAdmissionRef{
			ProfileRef:           state.CodexProfileRef{ID: created.ID, Revision: created.Revision},
			ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: created.ID, Revision: created.ContextPreference.Revision},
		},
	}
	references = performAdminRequest(t, app, http.MethodGet, "/api/admin/codex/profiles/"+created.ID+"/references", "")
	if references.Code != http.StatusOK || strings.Contains(references.Body.String(), "secret with spaces") {
		t.Fatalf("pending references response status=%d body=%s", references.Code, references.Body.String())
	}
	if err := json.NewDecoder(references.Body).Decode(&referencesResponse); err != nil {
		t.Fatalf("decode pending references: %v", err)
	}
	if len(referencesResponse.References) != 1 || referencesResponse.References[0].Kind != "pending_headless" {
		t.Fatalf("expected pending headless reference, got %#v", referencesResponse)
	}
	app.service.Surface("surface-1").PendingHeadless = nil

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Codex.Providers) != 0 || len(loaded.Config.Codex.Profiles) != 1 {
		t.Fatalf("canonical writer used wrong config owner: %#v", loaded.Config.Codex)
	}
	secret, ok := config.CurrentCodexAPIProfile(loaded.Config.Codex.Profiles[0])
	if !ok || secret.APIKey != " secret with spaces " {
		t.Fatalf("secret profile was not stored exactly: %#v ok=%v", secret, ok)
	}

	duplicate := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/profiles", `{
  "name":" team proxy ",
  "baseURL":"https://other.example/v1",
  "apiKey":"different",
  "model":"gpt-5.4",
  "reasoningEffort":"high"
}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d body=%s", duplicate.Code, duplicate.Body.String())
	}

	updatePath := "/api/admin/codex/profiles/" + created.ID
	missingPrecondition := performAdminRequest(t, app, http.MethodPut, updatePath, `{
  "name":"Team Proxy 2",
  "baseURL":"https://proxy.example/v1",
  "model":"gpt-5.5",
  "reasoningEffort":"high"
}`)
	if missingPrecondition.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing precondition status = %d body=%s", missingPrecondition.Code, missingPrecondition.Body.String())
	}
	assertAdminAPIErrorCode(t, missingPrecondition, "profile_revision_required")

	stale := performAdminRequestWithIfMatch(t, app, http.MethodPut, updatePath, `{
  "name":"Team Proxy 2",
  "baseURL":"https://proxy.example/v1",
  "model":"gpt-5.5",
  "reasoningEffort":"high"
}`, `"stale"`)
	if stale.Code != http.StatusPreconditionFailed || stale.Header().Get("ETag") != created.ETag {
		t.Fatalf("stale update status=%d etag=%q body=%s", stale.Code, stale.Header().Get("ETag"), stale.Body.String())
	}
	staleError := assertAdminAPIErrorCode(t, stale, "profile_revision_conflict")
	if staleError.Details == nil || strings.Contains(stale.Body.String(), "secret with spaces") {
		t.Fatalf("stale update omitted redacted current profile: %s", stale.Body.String())
	}

	update := performAdminRequestWithIfMatch(t, app, http.MethodPut, updatePath, `{
  "name":"Team Proxy 2",
  "baseURL":"https://proxy.example/v1",
  "model":"gpt-5.5",
  "reasoningEffort":"high"
}`, created.ETag)
	if update.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", update.Code, update.Body.String())
	}
	var updateResponse codexProfileResponse
	if err := json.NewDecoder(update.Body).Decode(&updateResponse); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	updated := updateResponse.Profile
	if updated.ID != created.ID || updated.Revision != 2 || updated.ETag == created.ETag || !updated.HasAPIKey {
		t.Fatalf("unexpected updated profile: %#v", updated)
	}
	loadedAfterUpdate, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath after update: %v", err)
	}
	updatedRecord := loadedAfterUpdate.Config.Codex.Profiles[config.IndexOfCodexAPIProfile(loadedAfterUpdate.Config.Codex.Profiles, created.ID)]
	if len(updatedRecord.Revisions) != 1 || updatedRecord.Revisions[0].Revision != updated.Revision {
		t.Fatalf("unreferenced old profile revision was not pruned: %#v", updatedRecord.Revisions)
	}

	contextPath := "/api/admin/codex/profiles/" + config.CodexNativeProfileID + "/context-preference"
	missingContextPrecondition := performAdminRequest(t, app, http.MethodPut, contextPath, `{"mode":"price_guard_272k"}`)
	if missingContextPrecondition.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing context precondition status = %d body=%s", missingContextPrecondition.Code, missingContextPrecondition.Body.String())
	}
	assertAdminAPIErrorCode(t, missingContextPrecondition, "profile_preference_revision_required")
	staleContext := performAdminRequestWithIfMatch(t, app, http.MethodPut, contextPath, `{"mode":"price_guard_272k"}`, created.ETag)
	if staleContext.Code != http.StatusPreconditionFailed || staleContext.Header().Get("ETag") != native.ContextPreference.ETag {
		t.Fatalf("stale context status=%d etag=%q body=%s", staleContext.Code, staleContext.Header().Get("ETag"), staleContext.Body.String())
	}
	staleContextError := assertAdminAPIErrorCode(t, staleContext, "profile_preference_revision_conflict")
	if staleContextError.Details == nil {
		t.Fatalf("stale context omitted current preference: %s", staleContext.Body.String())
	}
	contextUpdate := performAdminRequestWithIfMatch(t, app, http.MethodPut, contextPath, `{"mode":"price_guard_272k"}`, native.ContextPreference.ETag)
	if contextUpdate.Code != http.StatusOK {
		t.Fatalf("native context update status = %d body=%s", contextUpdate.Code, contextUpdate.Body.String())
	}
	var contextResponse codexContextPreferenceResponse
	if err := json.NewDecoder(contextUpdate.Body).Decode(&contextResponse); err != nil {
		t.Fatalf("decode context update: %v", err)
	}
	if contextResponse.ContextPreference.Revision != 2 || contextResponse.ContextPreference.Mode != state.CodexContextModePrice272K {
		t.Fatalf("unexpected context response: %#v", contextResponse)
	}

	deleteMissingPrecondition := performAdminRequest(t, app, http.MethodDelete, updatePath, "")
	if deleteMissingPrecondition.Code != http.StatusPreconditionRequired {
		t.Fatalf("delete without precondition status = %d body=%s", deleteMissingPrecondition.Code, deleteMissingPrecondition.Body.String())
	}
	assertAdminAPIErrorCode(t, deleteMissingPrecondition, "profile_revision_required")
	deleted := performAdminRequestWithIfMatch(t, app, http.MethodDelete, updatePath, "", updated.ETag)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestAdminCodexProfileUpdateRetainsReferencedOldRevision(t *testing.T) {
	app, configPath := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/profiles", `{
  "name":"Team Proxy",
  "baseURL":"https://old.example/v1",
  "apiKey":"old-secret",
  "model":"gpt-5.4",
  "reasoningEffort":"high"
}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var createResponse codexProfileResponse
	if err := json.NewDecoder(create.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	created := createResponse.Profile
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.Surface("surface-1").PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:      "inst-headless",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: created.ID,
		CodexAdmissionRef: &state.CodexAdmissionRef{
			ProfileRef:           state.CodexProfileRef{ID: created.ID, Revision: 1},
			ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: created.ID, Revision: 1},
		},
	}

	update := performAdminRequestWithIfMatch(t, app, http.MethodPut, "/api/admin/codex/profiles/"+created.ID, `{
  "name":"Team Proxy",
  "baseURL":"https://new.example/v1",
  "model":"gpt-5.5",
  "reasoningEffort":"medium"
}`, created.ETag)
	if update.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", update.Code, update.Body.String())
	}
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	record := loaded.Config.Codex.Profiles[config.IndexOfCodexAPIProfile(loaded.Config.Codex.Profiles, created.ID)]
	if len(record.Revisions) != 2 {
		t.Fatalf("referenced old profile revision was pruned: %#v", record.Revisions)
	}
	if record.Revisions[0].Revision != 1 || record.Revisions[1].Revision != 2 {
		t.Fatalf("unexpected retained revisions: %#v", record.Revisions)
	}
}

func TestAdminCodexProfileDeleteInUseReturnsRedactedReferences(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/profiles", `{
  "name":"Team Proxy",
  "baseURL":"https://api.example/v1",
  "apiKey":"secret with spaces",
  "model":"gpt-5.4",
  "reasoningEffort":"high"
}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var createResponse codexProfileResponse
	if err := json.NewDecoder(create.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	created := createResponse.Profile
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.Surface("surface-1").PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:      "inst-headless",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: created.ID,
		CodexAdmissionRef: &state.CodexAdmissionRef{
			ProfileRef:           state.CodexProfileRef{ID: created.ID, Revision: created.Revision},
			ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: created.ID, Revision: created.ContextPreference.Revision},
		},
	}

	deleted := performAdminRequestWithIfMatch(t, app, http.MethodDelete, "/api/admin/codex/profiles/"+created.ID, "", created.ETag)
	if deleted.Code != http.StatusConflict || strings.Contains(deleted.Body.String(), "secret with spaces") {
		t.Fatalf("delete in-use status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	errPayload := assertAdminAPIErrorCode(t, deleted, "codex_profile_in_use")
	detailsRaw, err := json.Marshal(errPayload.Details)
	if err != nil {
		t.Fatalf("marshal delete details: %v", err)
	}
	var details codexProfileReferencesResponse
	if err := json.Unmarshal(detailsRaw, &details); err != nil {
		t.Fatalf("decode delete in-use details: %v raw=%s", err, string(detailsRaw))
	}
	if details.ProfileID != created.ID || len(details.References) != 1 || details.References[0].Kind != "pending_headless" {
		t.Fatalf("expected delete in-use references in details, got %#v", details)
	}
}

func TestAdminCodexProfileUpdateRefreshesSurfaceDesiredRuntimeContract(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/profiles", `{
  "name":"Team Proxy",
  "baseURL":"https://old.example/v1",
  "apiKey":"old-secret",
  "model":"gpt-5.4",
  "reasoningEffort":"high"
}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var createResponse codexProfileResponse
	if err := json.NewDecoder(create.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	created := createResponse.Profile
	oldConnection := state.CodexConnectionContract{
		ProfileRef:           state.CodexProfileRef{ID: created.ID, Revision: 1},
		ConnectionGeneration: 1,
		ConnectionContractID: "conn-old",
		Kind:                 state.CodexProfileKindAPI,
		CapabilitySet:        codexprofile.CodexProfileCapabilitySetV1,
	}
	app.service.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract(created.ID), "", "")
	surface := app.service.Surface("surface-1")
	surface.CodexAdmissionRef = &state.CodexAdmissionRef{
		ProfileRef:           state.CodexProfileRef{ID: created.ID, Revision: 1},
		ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: created.ID, Revision: 1},
	}
	surface.CodexConnectionContract = state.CloneCodexConnectionContract(&oldConnection)
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-old",
		Backend:                 agentproto.BackendCodex,
		CodexProviderID:         created.ID,
		CodexAdmissionRef:       state.NormalizeCodexAdmissionRef(surface.CodexAdmissionRef),
		CodexConnectionContract: state.CloneCodexConnectionContract(&oldConnection),
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		Online:                  true,
		Threads:                 map[string]*state.ThreadRecord{},
	})

	update := performAdminRequestWithIfMatch(t, app, http.MethodPut, "/api/admin/codex/profiles/"+created.ID, `{
  "name":"Team Proxy",
  "baseURL":"https://new.example/v1",
  "model":"gpt-5.5",
  "reasoningEffort":"medium"
}`, created.ETag)
	if update.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", update.Code, update.Body.String())
	}
	if surface.CodexAdmissionRef == nil || surface.CodexAdmissionRef.ProfileRef.Revision != 2 {
		t.Fatalf("surface desired admission was not refreshed after profile update: %#v", surface.CodexAdmissionRef)
	}
	if surface.CodexConnectionContract == nil || surface.CodexConnectionContract.ConnectionContractID == "" ||
		surface.CodexConnectionContract.ConnectionContractID == oldConnection.ConnectionContractID {
		t.Fatalf("surface desired connection contract was not refreshed: %#v", surface.CodexConnectionContract)
	}
	if app.service.Surface("surface-1") == nil || app.service.Instance("inst-old") == nil {
		t.Fatal("test setup lost surface or instance")
	}
	if app.service.Surface("surface-1").CodexConnectionContract.ConnectionContractID == app.service.Instance("inst-old").CodexConnectionContract.ConnectionContractID {
		t.Fatalf("old instance should keep stale observed contract")
	}
}

func TestAdminCodexContextPreferenceUpdateRefreshesSurfaceDesiredRuntimeContract(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/profiles", `{
  "name":"Team Proxy",
  "baseURL":"https://api.example/v1",
  "apiKey":"secret",
  "model":"gpt-5.4",
  "reasoningEffort":"high"
}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var createResponse codexProfileResponse
	if err := json.NewDecoder(create.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	created := createResponse.Profile
	app.service.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract(created.ID), "", "")
	surface := app.service.Surface("surface-1")
	surface.CodexAdmissionRef = &state.CodexAdmissionRef{
		ProfileRef:           state.CodexProfileRef{ID: created.ID, Revision: created.Revision},
		ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: created.ID, Revision: created.ContextPreference.Revision},
	}
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{
		ThreadPolicyID:     "old-policy",
		ContextMode:        state.CodexContextModePrice272K,
		ContextWindow:      272000,
		AutoCompactLimit:   244800,
		PreferenceRevision: created.ContextPreference.Revision,
	}

	update := performAdminRequestWithIfMatch(t, app, http.MethodPut, "/api/admin/codex/profiles/"+created.ID+"/context-preference", `{"mode":"extended_1m"}`, created.ContextPreference.ETag)
	if update.Code != http.StatusOK {
		t.Fatalf("context preference update status = %d body=%s", update.Code, update.Body.String())
	}
	if surface.CodexAdmissionRef == nil || surface.CodexAdmissionRef.ContextPreferenceRef.Revision != created.ContextPreference.Revision+1 {
		t.Fatalf("surface desired admission preference ref was not refreshed: %#v", surface.CodexAdmissionRef)
	}
	if surface.CodexThreadPolicy == nil || surface.CodexThreadPolicy.PreferenceRevision != created.ContextPreference.Revision+1 ||
		surface.CodexThreadPolicy.ContextMode != state.CodexContextModeExtended ||
		surface.CodexThreadPolicy.ContextWindow != 1000000 {
		t.Fatalf("surface desired thread policy was not refreshed: %#v", surface.CodexThreadPolicy)
	}
}

func TestAdminCodexProfilesRejectIncompleteNewDefinition(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	for _, body := range []string{
		`{"name":"No Model","baseURL":"https://proxy.example/v1","apiKey":"secret","reasoningEffort":"high"}`,
		`{"name":"No Reasoning","baseURL":"https://proxy.example/v1","apiKey":"secret","model":"gpt-5.4"}`,
	} {
		rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/profiles", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("incomplete create status = %d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestLegacyCodexProviderAPIWritesOnlyCanonicalProfileStore(t *testing.T) {
	app, configPath := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":"Legacy Client Proxy",
  "baseURL":"https://proxy.example/v1",
  "apiKey":"secret",
  "model":"gpt-5.4",
  "reasoningEffort":"high"
}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("legacy create status = %d body=%s", create.Code, create.Body.String())
	}
	var response codexProviderResponse
	if err := json.NewDecoder(create.Body).Decode(&response); err != nil {
		t.Fatalf("decode legacy create: %v", err)
	}
	if !strings.HasPrefix(response.Provider.ID, "cp_") || response.Provider.ReadOnly || !response.Provider.HasAPIKey {
		t.Fatalf("unexpected legacy create projection: %#v", response.Provider)
	}
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Codex.Providers) != 0 || len(loaded.Config.Codex.Profiles) != 1 {
		t.Fatalf("legacy API created a second writer: %#v", loaded.Config.Codex)
	}

	duplicate := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":"legacy client proxy",
  "baseURL":"https://other.example/v1",
  "apiKey":"different",
  "model":"gpt-5.4",
  "reasoningEffort":"high"
}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("legacy duplicate status = %d body=%s", duplicate.Code, duplicate.Body.String())
	}

	update := performAdminRequest(t, app, http.MethodPut, "/api/admin/codex/providers/"+response.Provider.ID, `{
  "name":"Legacy Client Proxy 2",
  "baseURL":"https://proxy.example/v1",
  "model":"gpt-5.5",
  "reasoningEffort":"xhigh"
}`)
	if update.Code != http.StatusOK {
		t.Fatalf("legacy update status = %d body=%s", update.Code, update.Body.String())
	}
	var updated codexProviderResponse
	if err := json.NewDecoder(update.Body).Decode(&updated); err != nil {
		t.Fatalf("decode legacy update: %v", err)
	}
	if updated.Provider.ID != response.Provider.ID || updated.Provider.Name != "Legacy Client Proxy 2" {
		t.Fatalf("legacy adapter changed stable identity: %#v", updated.Provider)
	}

	deleted := performAdminRequest(t, app, http.MethodDelete, "/api/admin/codex/providers/"+response.Provider.ID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("legacy delete status = %d body=%s", deleted.Code, deleted.Body.String())
	}
}

func performAdminRequestWithIfMatch(t *testing.T, app *App, method, path, body, etag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("If-Match", etag)
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	return rec
}

func assertAdminAPIErrorCode(t *testing.T, rec *httptest.ResponseRecorder, code string) apiError {
	t.Helper()
	var payload apiErrorPayload
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode API error: %v body=%s", err, rec.Body.String())
	}
	if payload.Error.Code != code {
		t.Fatalf("API error code = %q, want %q body=%s", payload.Error.Code, code, rec.Body.String())
	}
	return payload.Error
}
