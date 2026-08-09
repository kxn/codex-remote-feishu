package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/profilecontextstate"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

type adminOpenCodeProfileView struct {
	ID                string `json:"id"`
	Revision          uint64 `json:"revision,omitempty"`
	ETag              string `json:"etag,omitempty"`
	Name              string `json:"name,omitempty"`
	BaseURL           string `json:"baseURL,omitempty"`
	APIKey            string `json:"apiKey,omitempty"`
	HasAPIKey         bool   `json:"hasAPIKey"`
	Model             string `json:"model,omitempty"`
	SmallModel        string `json:"smallModel,omitempty"`
	ReviewModel       string `json:"reviewModel,omitempty"`
	SubagentModel     string `json:"subagentModel,omitempty"`
	Instruction       string `json:"instruction,omitempty"`
	ReasoningEffort   string `json:"reasoningEffort,omitempty"`
	ProjectConfigMode string `json:"projectConfigMode,omitempty"`
	DataIsolationMode string `json:"dataIsolationMode,omitempty"`
	PermissionMode    string `json:"permissionMode,omitempty"`
	Available         bool   `json:"available"`
	StatusCode        string `json:"statusCode,omitempty"`
	BuiltIn           bool   `json:"builtIn,omitempty"`
	Persisted         bool   `json:"persisted"`
	ReadOnly          bool   `json:"readOnly,omitempty"`
}

type opencodeProfilesResponse struct {
	Profiles []adminOpenCodeProfileView `json:"profiles"`
}

type opencodeProfileResponse struct {
	Profile adminOpenCodeProfileView `json:"profile"`
}

type opencodeProfileReference struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type opencodeProfileReferencesResponse struct {
	ProfileID  string                     `json:"profileID"`
	References []opencodeProfileReference `json:"references"`
}

type opencodeProfileInUseError struct {
	ProfileID  string
	References []opencodeProfileReference
}

func (e *opencodeProfileInUseError) Error() string {
	return "profile_in_use"
}

type opencodeProfileWriteRequest struct {
	Name              *string `json:"name"`
	BaseURL           *string `json:"baseURL"`
	APIKey            *string `json:"apiKey"`
	Model             *string `json:"model"`
	SmallModel        *string `json:"smallModel"`
	ReviewModel       *string `json:"reviewModel"`
	SubagentModel     *string `json:"subagentModel"`
	Instruction       *string `json:"instruction"`
	ReasoningEffort   *string `json:"reasoningEffort"`
	ProjectConfigMode *string `json:"projectConfigMode"`
	DataIsolationMode *string `json:"dataIsolationMode"`
	PermissionMode    *string `json:"permissionMode"`
}

func (a *App) handleOpenCodeProfilesList(w http.ResponseWriter, _ *http.Request) {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{Code: "config_unavailable", Message: "failed to load config", Details: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, opencodeProfilesResponse{Profiles: adminOpenCodeProfileViews(loaded.Config)})
}

func (a *App) handleOpenCodeProfileCreate(w http.ResponseWriter, r *http.Request) {
	var req opencodeProfileWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "failed to decode opencode profile payload", Details: err.Error()})
		return
	}
	a.adminConfigMu.Lock()
	profile, err := a.createOpenCodeProfileLocked(openCodeAPIProfileInputFromRequest(req))
	a.adminConfigMu.Unlock()
	if err != nil {
		writeOpenCodeProfileServiceError(w, err)
		return
	}
	w.Header().Set("ETag", profile.ETag)
	writeJSON(w, http.StatusCreated, opencodeProfileResponse{Profile: profile})
}

func (a *App) handleOpenCodeProfileUpdate(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("If-Match")) == "" {
		writeAPIError(w, http.StatusPreconditionRequired, apiError{Code: "profile_revision_required", Message: "If-Match is required"})
		return
	}
	var req opencodeProfileWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "failed to decode opencode profile payload", Details: err.Error()})
		return
	}
	a.adminConfigMu.Lock()
	profile, currentETag, err := a.updateOpenCodeProfileLocked(r.PathValue("id"), r.Header.Get("If-Match"), openCodeAPIProfileInputFromRequest(req))
	a.adminConfigMu.Unlock()
	if currentETag != "" {
		w.Header().Set("ETag", currentETag)
	}
	if err != nil {
		if errors.Is(err, profilecontextstate.ErrETagMismatch) {
			writeAPIError(w, http.StatusPreconditionFailed, apiError{Code: "profile_revision_conflict", Message: "the profile changed; reload and retry", Details: map[string]any{"profile": profile}})
			return
		}
		writeOpenCodeProfileServiceError(w, err)
		return
	}
	w.Header().Set("ETag", profile.ETag)
	writeJSON(w, http.StatusOK, opencodeProfileResponse{Profile: profile})
}

func (a *App) handleOpenCodeProfileDelete(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("If-Match")) == "" {
		writeAPIError(w, http.StatusPreconditionRequired, apiError{Code: "profile_revision_required", Message: "If-Match is required"})
		return
	}
	a.adminConfigMu.Lock()
	profile, currentETag, err := a.deleteOpenCodeProfileLocked(r.PathValue("id"), r.Header.Get("If-Match"))
	a.adminConfigMu.Unlock()
	if currentETag != "" {
		w.Header().Set("ETag", currentETag)
	}
	if err != nil {
		if errors.Is(err, profilecontextstate.ErrETagMismatch) {
			writeAPIError(w, http.StatusPreconditionFailed, apiError{Code: "profile_revision_conflict", Message: "the profile changed; reload and retry", Details: map[string]any{"profile": profile}})
			return
		}
		writeOpenCodeProfileServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleOpenCodeProfileReferences(w http.ResponseWriter, r *http.Request) {
	profileID := config.NormalizeOpenCodeProfileID(r.PathValue("id"))
	a.adminConfigMu.Lock()
	loaded, err := a.loadAdminConfig()
	_, found := findOpenCodeProfileView(adminOpenCodeProfileViews(loaded.Config), profileID)
	var references []opencodeProfileReference
	if err == nil && !found {
		err = fmt.Errorf("profile_not_found")
	}
	if err == nil {
		references = a.openCodeProfileReferencesLocked(profileID)
	}
	a.adminConfigMu.Unlock()
	if err != nil {
		writeOpenCodeProfileServiceError(w, err)
		return
	}
	if references == nil {
		references = []opencodeProfileReference{}
	}
	writeJSON(w, http.StatusOK, opencodeProfileReferencesResponse{ProfileID: profileID, References: references})
}

func (a *App) createOpenCodeProfileLocked(input config.OpenCodeAPIProfileInput) (adminOpenCodeProfileView, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return adminOpenCodeProfileView{}, fmt.Errorf("config_unavailable: %w", err)
	}
	record, err := config.PrepareOpenCodeAPIProfileCreate(loaded.Config.OpenCode.Profiles, input)
	if err != nil {
		return adminOpenCodeProfileView{}, err
	}
	updated := loaded.Config
	updated.OpenCode.Profiles = append(updated.OpenCode.Profiles, record)
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		return adminOpenCodeProfileView{}, fmt.Errorf("config_write_failed: %w", err)
	}
	current, _ := config.CurrentOpenCodeAPIProfile(record)
	return adminOpenCodeProfileViewFromProfile(config.OpenCodeProfile{OpenCodeAPIProfileSecretConfig: current}), nil
}

func (a *App) updateOpenCodeProfileLocked(profileID, expectedETag string, input config.OpenCodeAPIProfileInput) (adminOpenCodeProfileView, string, error) {
	profileID = config.NormalizeOpenCodeProfileID(profileID)
	if profileID == config.OpenCodeDefaultProfileID {
		return adminOpenCodeProfileView{}, "", fmt.Errorf("opencode_profile_read_only")
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return adminOpenCodeProfileView{}, "", fmt.Errorf("config_unavailable: %w", err)
	}
	index := config.IndexOfOpenCodeAPIProfile(loaded.Config.OpenCode.Profiles, profileID)
	if index < 0 {
		return adminOpenCodeProfileView{}, "", fmt.Errorf("profile_not_found")
	}
	record := loaded.Config.OpenCode.Profiles[index]
	current, ok := config.CurrentOpenCodeAPIProfile(record)
	if !ok {
		return adminOpenCodeProfileView{}, "", fmt.Errorf("profile_catalog_degraded: current revision missing")
	}
	currentView := adminOpenCodeProfileViewFromProfile(config.OpenCodeProfile{OpenCodeAPIProfileSecretConfig: current})
	currentETag := currentView.ETag
	if expectedETag != currentETag {
		return currentView, currentETag, profilecontextstate.ErrETagMismatch
	}
	if err := config.ValidateOpenCodeAPIProfileNameUnique(loaded.Config.OpenCode.Profiles, profileID, input.Name); err != nil {
		return adminOpenCodeProfileView{}, currentETag, err
	}
	record, changed, err := config.PrepareOpenCodeAPIProfileUpdate(record, input)
	if err != nil {
		return adminOpenCodeProfileView{}, currentETag, err
	}
	if changed {
		updated := loaded.Config
		updated.OpenCode.Profiles[index] = record
		if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
			return adminOpenCodeProfileView{}, currentETag, fmt.Errorf("config_write_failed: %w", err)
		}
		record = updated.OpenCode.Profiles[index]
	}
	current, _ = config.CurrentOpenCodeAPIProfile(record)
	view := adminOpenCodeProfileViewFromProfile(config.OpenCodeProfile{OpenCodeAPIProfileSecretConfig: current})
	return view, view.ETag, nil
}

func (a *App) deleteOpenCodeProfileLocked(profileID, expectedETag string) (adminOpenCodeProfileView, string, error) {
	profileID = config.NormalizeOpenCodeProfileID(profileID)
	if profileID == config.OpenCodeDefaultProfileID {
		return adminOpenCodeProfileView{}, "", fmt.Errorf("opencode_profile_read_only")
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return adminOpenCodeProfileView{}, "", fmt.Errorf("config_unavailable: %w", err)
	}
	index := config.IndexOfOpenCodeAPIProfile(loaded.Config.OpenCode.Profiles, profileID)
	if index < 0 {
		return adminOpenCodeProfileView{}, "", fmt.Errorf("profile_not_found")
	}
	current, ok := config.CurrentOpenCodeAPIProfile(loaded.Config.OpenCode.Profiles[index])
	if !ok {
		return adminOpenCodeProfileView{}, "", fmt.Errorf("profile_catalog_degraded: current revision missing")
	}
	currentView := adminOpenCodeProfileViewFromProfile(config.OpenCodeProfile{OpenCodeAPIProfileSecretConfig: current})
	if expectedETag != currentView.ETag {
		return currentView, currentView.ETag, profilecontextstate.ErrETagMismatch
	}
	if references := a.openCodeProfileReferencesLocked(profileID); len(references) > 0 {
		return currentView, currentView.ETag, &opencodeProfileInUseError{ProfileID: profileID, References: references}
	}
	updated := loaded.Config
	updated.OpenCode.Profiles = append(append([]config.OpenCodeAPIProfileRecord{}, updated.OpenCode.Profiles[:index]...), updated.OpenCode.Profiles[index+1:]...)
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		return currentView, currentView.ETag, fmt.Errorf("config_write_failed: %w", err)
	}
	return currentView, currentView.ETag, nil
}

func adminOpenCodeProfileViews(cfg config.AppConfig) []adminOpenCodeProfileView {
	profiles := config.ListOpenCodeProfiles(cfg)
	views := make([]adminOpenCodeProfileView, 0, len(profiles))
	for _, profile := range profiles {
		views = append(views, adminOpenCodeProfileViewFromProfile(profile))
	}
	return views
}

func adminOpenCodeProfileViewFromProfile(profile config.OpenCodeProfile) adminOpenCodeProfileView {
	status := ""
	available := true
	if !profile.BuiltIn {
		status = config.OpenCodeAPIProfileStatus(profile.OpenCodeAPIProfileSecretConfig)
		available = status == ""
	}
	view := adminOpenCodeProfileView{
		ID:                profile.ID,
		Revision:          profile.Revision,
		ETag:              state.OpenCodeProfileDefinitionETag(profile.ID, profile.Revision),
		Name:              profile.Name,
		BaseURL:           profile.BaseURL,
		HasAPIKey:         strings.TrimSpace(profile.APIKey) != "",
		Model:             profile.Model,
		SmallModel:        profile.SmallModel,
		ReviewModel:       profile.ReviewModel,
		SubagentModel:     profile.SubagentModel,
		Instruction:       profile.Instruction,
		ReasoningEffort:   profile.ReasoningEffort,
		ProjectConfigMode: profile.ProjectConfigMode,
		DataIsolationMode: profile.DataIsolationMode,
		PermissionMode:    profile.PermissionMode,
		Available:         available,
		StatusCode:        status,
		BuiltIn:           profile.BuiltIn,
		Persisted:         !profile.BuiltIn,
		ReadOnly:          profile.BuiltIn,
	}
	return view
}

func openCodeAPIProfileInputFromRequest(req opencodeProfileWriteRequest) config.OpenCodeAPIProfileInput {
	return config.OpenCodeAPIProfileInput{
		Name:              optionalStringValue(req.Name),
		BaseURL:           optionalStringValue(req.BaseURL),
		APIKey:            optionalStringValue(req.APIKey),
		Model:             optionalStringValue(req.Model),
		SmallModel:        optionalStringValue(req.SmallModel),
		ReviewModel:       optionalStringValue(req.ReviewModel),
		SubagentModel:     optionalStringValue(req.SubagentModel),
		Instruction:       optionalStringValue(req.Instruction),
		ReasoningEffort:   optionalStringValue(req.ReasoningEffort),
		ProjectConfigMode: optionalStringValue(req.ProjectConfigMode),
		DataIsolationMode: optionalStringValue(req.DataIsolationMode),
		PermissionMode:    optionalStringValue(req.PermissionMode),
	}
}

func findOpenCodeProfileView(profiles []adminOpenCodeProfileView, profileID string) (adminOpenCodeProfileView, bool) {
	profileID = config.NormalizeOpenCodeProfileID(profileID)
	for _, profile := range profiles {
		if config.NormalizeOpenCodeProfileID(profile.ID) == profileID {
			return profile, true
		}
	}
	return adminOpenCodeProfileView{}, false
}

func (a *App) openCodeProfileReferencesLocked(profileID string) []opencodeProfileReference {
	profileID = config.NormalizeOpenCodeProfileID(profileID)
	a.mu.Lock()
	defer a.mu.Unlock()
	references := make([]opencodeProfileReference, 0)
	if a.botCapabilitySettingsState.store != nil {
		for _, record := range a.botCapabilitySettingsState.store.Entries() {
			if agentproto.NormalizeBackend(record.Backend) == agentproto.BackendOpenCode && config.NormalizeOpenCodeProfileID(record.OpenCodeProfileID) == profileID {
				references = append(references, opencodeProfileReference{Kind: "bot_default", Name: record.GatewayID, Reason: "selected as bot default"})
			}
		}
	}
	for _, surface := range a.service.Surfaces() {
		if surface == nil {
			continue
		}
		if agentproto.NormalizeBackend(surface.Backend) == agentproto.BackendOpenCode && config.NormalizeOpenCodeProfileID(surface.OpenCodeProfileID) == profileID {
			references = append(references, opencodeProfileReference{Kind: "surface_desired", Name: surface.SurfaceSessionID, Reason: "selected for surface"})
		}
		if openCodeAdmissionRefProfileID(surface.OpenCodeAdmissionRef) == profileID {
			references = append(references, opencodeProfileReference{Kind: "surface_actual", Name: surface.SurfaceSessionID, Reason: "active route admission"})
		}
		if pending := surface.PendingHeadless; pending != nil && openCodeAdmissionRefProfileID(pending.OpenCodeAdmissionRef) == profileID {
			references = append(references, opencodeProfileReference{Kind: "pending_headless", Name: xutil.FirstNonEmpty(pending.InstanceID, surface.SurfaceSessionID), Reason: "pending headless launch"})
		}
		for _, item := range surface.QueueItems {
			if item == nil || openCodeAdmissionRefProfileID(item.OpenCodeAdmissionRef) != profileID {
				continue
			}
			kind := "queue_item"
			if item.ID == surface.ActiveQueueItemID {
				kind = "active_queue_item"
			}
			references = append(references, opencodeProfileReference{Kind: kind, Name: xutil.FirstNonEmpty(item.ID, surface.SurfaceSessionID), Reason: "queued prompt admission"})
		}
	}
	for _, inst := range a.service.Instances() {
		if inst == nil {
			continue
		}
		if agentproto.NormalizeBackend(inst.Backend) == agentproto.BackendOpenCode && config.NormalizeOpenCodeProfileID(inst.OpenCodeProfileID) == profileID {
			references = append(references, opencodeProfileReference{Kind: "active_instance", Name: inst.InstanceID, Reason: "observed instance profile"})
			continue
		}
		if openCodeAdmissionRefProfileID(inst.OpenCodeAdmissionRef) == profileID {
			references = append(references, opencodeProfileReference{Kind: "active_instance", Name: inst.InstanceID, Reason: "observed instance admission"})
		}
	}
	return references
}

func openCodeAdmissionRefProfileID(ref *state.OpenCodeAdmissionRef) string {
	ref = state.NormalizeOpenCodeAdmissionRef(ref)
	if ref == nil {
		return ""
	}
	return config.NormalizeOpenCodeProfileID(ref.ProfileRef.ID)
}

func writeOpenCodeProfileServiceError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if inUse := (&opencodeProfileInUseError{}); errors.As(err, &inUse) {
		writeAPIError(w, http.StatusConflict, apiError{Code: "profile_in_use", Message: "opencode profile is still in use", Details: opencodeProfileReferencesResponse{ProfileID: inUse.ProfileID, References: inUse.References}})
		return
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "profile_not_found"):
		writeAPIError(w, http.StatusNotFound, apiError{Code: "profile_not_found", Message: "opencode profile not found", Details: message})
	case strings.Contains(message, "read_only"):
		writeAPIError(w, http.StatusConflict, apiError{Code: "opencode_profile_read_only", Message: "this opencode profile is read-only", Details: message})
	case strings.Contains(message, "config_unavailable"), strings.Contains(message, "config_write_failed"):
		writeAPIError(w, http.StatusInternalServerError, apiError{Code: "config_write_failed", Message: "failed to save opencode profile config", Details: message})
	default:
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_opencode_profile", Message: "invalid opencode profile", Details: message})
	}
}
