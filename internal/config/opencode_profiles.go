package config

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	OpenCodeDefaultProfileID   = state.DefaultOpenCodeProfileID
	OpenCodeDefaultProfileName = "本机默认"
	OpenCodeProfileLimit       = 50

	OpenCodeBinaryEnv               = "OPENCODE_BIN"
	OpenCodeRuntimeProfileIDEnv     = "CODEX_REMOTE_OPENCODE_PROFILE_ID"
	OpenCodeRuntimeAccessModeEnv    = "CODEX_REMOTE_OPENCODE_RUNTIME_ACCESS_MODE"
	OpenCodeLaunchJSONEnv           = "CODEX_REMOTE_OPENCODE_LAUNCH_JSON"
	OpenCodeConfigContentEnv        = "OPENCODE_CONFIG_CONTENT"
	OpenCodeAuthContentEnv          = "OPENCODE_AUTH_CONTENT"
	OpenCodeDisableProjectConfigEnv = "OPENCODE_DISABLE_PROJECT_CONFIG"

	OpenCodeProjectConfigInherit = "inherit"
	OpenCodeProjectConfigDisable = "disable"

	OpenCodeDataIsolationInherit = "inherit"
	OpenCodeDataIsolationProcess = "process"

	OpenCodeProviderTypeOpenAICompatibleChat = "openai_compatible_chat"
	OpenCodeProviderTypeGoogleGemini         = "google_gemini"
)

type OpenCodeSettings struct {
	BinaryPath string                     `json:"binaryPath,omitempty"`
	Profiles   []OpenCodeAPIProfileRecord `json:"profiles,omitempty"`
}

type OpenCodeAPIProfileRecord struct {
	ID              string                           `json:"id"`
	CurrentRevision uint64                           `json:"currentRevision"`
	Revisions       []OpenCodeAPIProfileSecretConfig `json:"revisions"`
}

type OpenCodeAPIProfileSecretConfig struct {
	ID                   string `json:"id"`
	Revision             uint64 `json:"revision"`
	CredentialGeneration uint64 `json:"credentialGeneration"`
	ConnectionGeneration uint64 `json:"connectionGeneration"`
	Name                 string `json:"name"`
	ProviderType         string `json:"providerType,omitempty"`
	BaseURL              string `json:"baseURL"`
	APIKey               string `json:"apiKey"`
	Model                string `json:"model"`
	SmallModel           string `json:"smallModel,omitempty"`
	ReviewModel          string `json:"reviewModel,omitempty"`
	SubagentModel        string `json:"subagentModel,omitempty"`
	Instruction          string `json:"instruction,omitempty"`
	ReasoningEffort      string `json:"reasoningEffort,omitempty"`
	ProjectConfigMode    string `json:"projectConfigMode,omitempty"`
	DataIsolationMode    string `json:"dataIsolationMode,omitempty"`
	PermissionMode       string `json:"permissionMode,omitempty"`
	VisionSupported      bool   `json:"visionSupported,omitempty"`
}

type OpenCodeAPIProfileInput struct {
	Name              string
	ProviderType      string
	BaseURL           string
	APIKey            string
	Model             string
	SmallModel        string
	ReviewModel       string
	SubagentModel     string
	Instruction       string
	ReasoningEffort   string
	ProjectConfigMode string
	DataIsolationMode string
	PermissionMode    string
	VisionSupported   bool
}

type OpenCodeProfile struct {
	OpenCodeAPIProfileSecretConfig
	BuiltIn bool
}

func BuiltInOpenCodeProfile() OpenCodeProfile {
	return OpenCodeProfile{
		BuiltIn: true,
		OpenCodeAPIProfileSecretConfig: OpenCodeAPIProfileSecretConfig{
			ID:                   OpenCodeDefaultProfileID,
			Revision:             1,
			CredentialGeneration: 1,
			ConnectionGeneration: 1,
			Name:                 OpenCodeDefaultProfileName,
			ProjectConfigMode:    OpenCodeProjectConfigInherit,
			DataIsolationMode:    OpenCodeDataIsolationInherit,
		},
	}
}

func CurrentOpenCodeAPIProfile(record OpenCodeAPIProfileRecord) (OpenCodeAPIProfileSecretConfig, bool) {
	record.ID = CanonicalOpenCodeProfileID(record.ID)
	for _, revision := range record.Revisions {
		if revision.Revision == record.CurrentRevision && CanonicalOpenCodeProfileID(revision.ID) == record.ID {
			revision.ID = record.ID
			return normalizeOpenCodeAPIProfileRevision(revision), true
		}
	}
	return OpenCodeAPIProfileSecretConfig{}, false
}

func OpenCodeAPIProfileStatus(profile OpenCodeAPIProfileSecretConfig) string {
	profile = normalizeOpenCodeAPIProfileRevision(profile)
	if profile.ProviderType == "" || profile.Model == "" ||
		(profile.ProviderType == OpenCodeProviderTypeOpenAICompatibleChat && profile.BaseURL == "") ||
		(profile.BaseURL != "" && validateProfileBaseURL(profile.BaseURL) != nil) {
		return "profile_definition_incomplete"
	}
	if profile.APIKey == "" {
		return "profile_secret_missing"
	}
	return ""
}

func ListOpenCodeProfiles(cfg AppConfig) []OpenCodeProfile {
	profiles := []OpenCodeProfile{BuiltInOpenCodeProfile()}
	for _, record := range NormalizeOpenCodeAPIProfileRecords(cfg.OpenCode.Profiles) {
		profile, ok := CurrentOpenCodeAPIProfile(record)
		if !ok {
			continue
		}
		profiles = append(profiles, OpenCodeProfile{OpenCodeAPIProfileSecretConfig: profile})
	}
	return profiles
}

func ResolveOpenCodeProfile(cfg AppConfig, profileID string) (OpenCodeProfile, bool) {
	profileID = NormalizeOpenCodeProfileID(profileID)
	if profileID == OpenCodeDefaultProfileID {
		return BuiltInOpenCodeProfile(), true
	}
	for _, profile := range ListOpenCodeProfiles(cfg) {
		if profile.BuiltIn {
			continue
		}
		if CanonicalOpenCodeProfileID(profile.ID) == profileID {
			return profile, true
		}
	}
	return OpenCodeProfile{}, false
}

func PrepareOpenCodeAPIProfileCreate(existing []OpenCodeAPIProfileRecord, input OpenCodeAPIProfileInput) (OpenCodeAPIProfileRecord, error) {
	if len(existing) >= OpenCodeProfileLimit {
		return OpenCodeAPIProfileRecord{}, fmt.Errorf("opencode profile catalog limit reached")
	}
	input, err := validateOpenCodeAPIProfileInput(input, true)
	if err != nil {
		return OpenCodeAPIProfileRecord{}, err
	}
	if err := validateOpenCodeProfileNameUnique(existing, "", input.Name); err != nil {
		return OpenCodeAPIProfileRecord{}, err
	}
	id, err := newOpenCodeProfileID(existing)
	if err != nil {
		return OpenCodeAPIProfileRecord{}, err
	}
	profile := OpenCodeAPIProfileSecretConfig{
		ID:                   id,
		Revision:             1,
		CredentialGeneration: 1,
		ConnectionGeneration: 1,
		Name:                 input.Name,
		ProviderType:         input.ProviderType,
		BaseURL:              input.BaseURL,
		APIKey:               input.APIKey,
		Model:                input.Model,
		SmallModel:           input.SmallModel,
		ReviewModel:          input.ReviewModel,
		SubagentModel:        input.SubagentModel,
		Instruction:          input.Instruction,
		ReasoningEffort:      input.ReasoningEffort,
		ProjectConfigMode:    input.ProjectConfigMode,
		DataIsolationMode:    input.DataIsolationMode,
		PermissionMode:       input.PermissionMode,
		VisionSupported:      input.VisionSupported,
	}
	return OpenCodeAPIProfileRecord{ID: id, CurrentRevision: 1, Revisions: []OpenCodeAPIProfileSecretConfig{profile}}, nil
}

func PrepareOpenCodeAPIProfileUpdate(record OpenCodeAPIProfileRecord, input OpenCodeAPIProfileInput) (OpenCodeAPIProfileRecord, bool, error) {
	current, ok := CurrentOpenCodeAPIProfile(record)
	if !ok {
		return record, false, fmt.Errorf("opencode profile current revision is missing")
	}
	if input.APIKey == "" {
		input.APIKey = current.APIKey
	}
	input, err := validateOpenCodeAPIProfileInput(input, true)
	if err != nil {
		return record, false, err
	}
	if current.Name == input.Name && current.ProviderType == input.ProviderType &&
		current.BaseURL == input.BaseURL && current.APIKey == input.APIKey &&
		current.Model == input.Model && current.SmallModel == input.SmallModel && current.ReviewModel == input.ReviewModel &&
		current.SubagentModel == input.SubagentModel && current.Instruction == input.Instruction &&
		current.ReasoningEffort == input.ReasoningEffort && current.ProjectConfigMode == input.ProjectConfigMode &&
		current.DataIsolationMode == input.DataIsolationMode && current.PermissionMode == input.PermissionMode &&
		current.VisionSupported == input.VisionSupported {
		return record, false, nil
	}
	next := current
	next.Revision = current.Revision + 1
	next.Name = input.Name
	next.ProviderType = input.ProviderType
	next.BaseURL = input.BaseURL
	next.Model = input.Model
	next.SmallModel = input.SmallModel
	next.ReviewModel = input.ReviewModel
	next.SubagentModel = input.SubagentModel
	next.Instruction = input.Instruction
	next.ReasoningEffort = input.ReasoningEffort
	next.VisionSupported = input.VisionSupported
	next.ProjectConfigMode = input.ProjectConfigMode
	next.DataIsolationMode = input.DataIsolationMode
	next.PermissionMode = input.PermissionMode
	if current.APIKey != input.APIKey {
		next.APIKey = input.APIKey
		next.CredentialGeneration++
	}
	if current.ProviderType != input.ProviderType || current.BaseURL != input.BaseURL || current.APIKey != input.APIKey {
		next.ConnectionGeneration++
	}
	record.ID = current.ID
	record.CurrentRevision = next.Revision
	record.Revisions = append(append([]OpenCodeAPIProfileSecretConfig{}, record.Revisions...), next)
	return record, true, nil
}

func IndexOfOpenCodeAPIProfile(records []OpenCodeAPIProfileRecord, profileID string) int {
	profileID = NormalizeOpenCodeProfileID(profileID)
	if profileID == OpenCodeDefaultProfileID {
		return -1
	}
	for index, record := range records {
		if CanonicalOpenCodeProfileID(record.ID) == profileID {
			return index
		}
	}
	return -1
}

func ValidateOpenCodeAPIProfileNameUnique(records []OpenCodeAPIProfileRecord, exceptID, name string) error {
	return validateOpenCodeProfileNameUnique(records, NormalizeOpenCodeProfileID(exceptID), name)
}

func NormalizeOpenCodeProfileID(value string) string {
	value = CanonicalOpenCodeProfileID(value)
	if value == "" {
		return OpenCodeDefaultProfileID
	}
	return value
}

func CanonicalOpenCodeProfileID(value string) string {
	return canonicalProfileID(value, '_')
}

func NormalizeOpenCodeAPIProfileRecords(records []OpenCodeAPIProfileRecord) []OpenCodeAPIProfileRecord {
	if len(records) == 0 {
		return nil
	}
	result := make([]OpenCodeAPIProfileRecord, 0, len(records))
	for _, record := range records {
		record.ID = CanonicalOpenCodeProfileID(record.ID)
		if record.ID == "" || record.CurrentRevision == 0 || record.ID == OpenCodeDefaultProfileID {
			continue
		}
		revisions := make([]OpenCodeAPIProfileSecretConfig, 0, len(record.Revisions))
		for _, revision := range record.Revisions {
			if CanonicalOpenCodeProfileID(revision.ID) != record.ID || revision.Revision == 0 {
				continue
			}
			revision.ID = record.ID
			revision = normalizeOpenCodeAPIProfileRevision(revision)
			revisions = append(revisions, revision)
		}
		record.Revisions = revisions
		if _, ok := CurrentOpenCodeAPIProfile(record); ok {
			result = append(result, record)
		}
	}
	return result
}

func ValidateOpenCodeAPIProfileRecords(records []OpenCodeAPIProfileRecord) error {
	if len(records) > OpenCodeProfileLimit {
		return fmt.Errorf("opencode profile catalog limit exceeded")
	}
	ids := make(map[string]struct{}, len(records))
	names := make([]string, 0, len(records))
	for _, record := range records {
		record.ID = CanonicalOpenCodeProfileID(record.ID)
		if record.ID == "" || record.ID == OpenCodeDefaultProfileID || record.CurrentRevision == 0 || len(record.Revisions) == 0 {
			return fmt.Errorf("invalid opencode profile record")
		}
		if _, exists := ids[record.ID]; exists {
			return fmt.Errorf("duplicate opencode profile id %q", record.ID)
		}
		ids[record.ID] = struct{}{}
		revisions := make(map[uint64]struct{}, len(record.Revisions))
		var current OpenCodeAPIProfileSecretConfig
		maxRevision := uint64(0)
		for _, revision := range record.Revisions {
			revisionID := CanonicalOpenCodeProfileID(revision.ID)
			if revisionID != record.ID || revision.Revision == 0 ||
				revision.CredentialGeneration == 0 || revision.ConnectionGeneration == 0 {
				return fmt.Errorf("invalid opencode profile revision for %q", record.ID)
			}
			if err := validateStoredOpenCodeAPIProfileRevision(revision); err != nil {
				return fmt.Errorf("invalid opencode profile revision for %q: %w", record.ID, err)
			}
			if _, exists := revisions[revision.Revision]; exists {
				return fmt.Errorf("duplicate opencode profile revision for %q", record.ID)
			}
			revisions[revision.Revision] = struct{}{}
			if revision.Revision > maxRevision {
				maxRevision = revision.Revision
			}
			if revision.Revision == record.CurrentRevision {
				current = normalizeOpenCodeAPIProfileRevision(revision)
			}
		}
		if err := validateProfileGenerations(record.Revisions, openCodeGenerationAccessors); err != nil {
			return fmt.Errorf("invalid opencode profile generations for %q: %w", record.ID, err)
		}
		if current.Revision == 0 || maxRevision != record.CurrentRevision {
			return fmt.Errorf("missing or stale current opencode profile revision for %q", record.ID)
		}
		for _, name := range names {
			if profileNameKey(name) == profileNameKey(current.Name) {
				return fmt.Errorf("duplicate opencode profile name")
			}
		}
		names = append(names, current.Name)
	}
	return nil
}

func normalizeOpenCodeAPIProfileRevision(profile OpenCodeAPIProfileSecretConfig) OpenCodeAPIProfileSecretConfig {
	profile.ID = CanonicalOpenCodeProfileID(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.ProviderType = normalizeOpenCodeProviderType(profile.ProviderType)
	profile.BaseURL = strings.TrimSpace(profile.BaseURL)
	profile.Model = strings.TrimSpace(profile.Model)
	profile.SmallModel = strings.TrimSpace(profile.SmallModel)
	profile.ReviewModel = strings.TrimSpace(profile.ReviewModel)
	profile.SubagentModel = strings.TrimSpace(profile.SubagentModel)
	profile.Instruction = strings.TrimSpace(profile.Instruction)
	profile.ReasoningEffort = strings.ToLower(strings.TrimSpace(profile.ReasoningEffort))
	profile.ProjectConfigMode = normalizeOpenCodeProjectConfigMode(profile.ProjectConfigMode)
	profile.DataIsolationMode = normalizeOpenCodeDataIsolationMode(profile.DataIsolationMode)
	profile.PermissionMode = strings.ToLower(strings.TrimSpace(profile.PermissionMode))
	return profile
}

func validateOpenCodeAPIProfileInput(input OpenCodeAPIProfileInput, requireKey bool) (OpenCodeAPIProfileInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ProviderType = normalizeOpenCodeProviderType(input.ProviderType)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.Model = strings.TrimSpace(input.Model)
	input.SmallModel = strings.TrimSpace(input.SmallModel)
	input.ReviewModel = strings.TrimSpace(input.ReviewModel)
	input.SubagentModel = strings.TrimSpace(input.SubagentModel)
	input.Instruction = strings.TrimSpace(input.Instruction)
	input.ReasoningEffort = strings.ToLower(strings.TrimSpace(input.ReasoningEffort))
	input.ProjectConfigMode = normalizeOpenCodeProjectConfigMode(input.ProjectConfigMode)
	input.DataIsolationMode = normalizeOpenCodeDataIsolationMode(input.DataIsolationMode)
	input.PermissionMode = strings.ToLower(strings.TrimSpace(input.PermissionMode))
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 64 || hasUnsafeProfileText(input.Name) {
		return input, fmt.Errorf("opencode profile name is invalid")
	}
	if input.ProviderType == "" {
		return input, fmt.Errorf("opencode profile providerType is invalid")
	}
	if input.BaseURL == "" && input.ProviderType == OpenCodeProviderTypeOpenAICompatibleChat {
		return input, fmt.Errorf("opencode profile baseURL is invalid")
	}
	if len(input.BaseURL) > 2048 {
		return input, fmt.Errorf("opencode profile baseURL is invalid")
	}
	if input.BaseURL != "" {
		if err := validateProfileBaseURL(input.BaseURL); err != nil {
			return input, fmt.Errorf("opencode profile baseURL is invalid: %w", err)
		}
	}
	for fieldName, value := range map[string]string{
		"model":           input.Model,
		"smallModel":      input.SmallModel,
		"reviewModel":     input.ReviewModel,
		"subagentModel":   input.SubagentModel,
		"reasoningEffort": input.ReasoningEffort,
		"permissionMode":  input.PermissionMode,
	} {
		limit := 256
		if fieldName == "reasoningEffort" || fieldName == "permissionMode" {
			limit = 64
		}
		if fieldName == "model" && value == "" {
			return input, fmt.Errorf("opencode profile model is invalid")
		}
		if len(value) > limit || hasUnsafeProfileText(value) {
			return input, fmt.Errorf("opencode profile %s is invalid", fieldName)
		}
	}
	if err := ValidateInstruction(input.Instruction); err != nil {
		return input, fmt.Errorf("opencode profile instruction is invalid: %w", err)
	}
	if requireKey && input.APIKey == "" {
		return input, fmt.Errorf("opencode profile apiKey is required")
	}
	if len(input.APIKey) > 16*1024 || strings.ContainsAny(input.APIKey, "\x00\r\n") {
		return input, fmt.Errorf("opencode profile apiKey is invalid")
	}
	return input, nil
}

func validateStoredOpenCodeAPIProfileRevision(revision OpenCodeAPIProfileSecretConfig) error {
	normalized := normalizeOpenCodeAPIProfileRevision(revision)
	if normalized.Name == "" || utf8.RuneCountInString(normalized.Name) > 64 || hasUnsafeProfileText(revision.Name) {
		return fmt.Errorf("invalid name")
	}
	if normalizeOpenCodeProviderType(revision.ProviderType) == "" {
		return fmt.Errorf("invalid providerType")
	}
	if (normalized.ProviderType == OpenCodeProviderTypeOpenAICompatibleChat && normalized.BaseURL == "") ||
		len(normalized.BaseURL) > 2048 ||
		(normalized.BaseURL != "" && validateProfileBaseURL(normalized.BaseURL) != nil) ||
		normalized.Model == "" || len(normalized.Model) > 256 || hasUnsafeProfileText(revision.Model) ||
		len(normalized.SmallModel) > 256 || hasUnsafeProfileText(revision.SmallModel) ||
		len(normalized.ReviewModel) > 256 || hasUnsafeProfileText(revision.ReviewModel) ||
		len(normalized.SubagentModel) > 256 || hasUnsafeProfileText(revision.SubagentModel) ||
		ValidateInstruction(revision.Instruction) != nil ||
		len(normalized.ReasoningEffort) > 64 || hasUnsafeProfileText(revision.ReasoningEffort) ||
		len(normalized.PermissionMode) > 64 || hasUnsafeProfileText(revision.PermissionMode) ||
		len(revision.APIKey) > 16*1024 || strings.ContainsAny(revision.APIKey, "\x00\r\n") {
		return fmt.Errorf("invalid field")
	}
	if revision.CredentialGeneration > revision.Revision || revision.ConnectionGeneration > revision.Revision ||
		revision.ConnectionGeneration < revision.CredentialGeneration {
		return fmt.Errorf("invalid generation")
	}
	return nil
}

func validateOpenCodeProfileNameUnique(records []OpenCodeAPIProfileRecord, exceptID, name string) error {
	nameKey := profileNameKey(name)
	for _, record := range records {
		if CanonicalOpenCodeProfileID(record.ID) == exceptID {
			continue
		}
		current, ok := CurrentOpenCodeAPIProfile(record)
		if ok && profileNameKey(current.Name) == nameKey {
			return fmt.Errorf("opencode profile name already exists")
		}
	}
	return nil
}

func normalizeOpenCodeProjectConfigMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case OpenCodeProjectConfigDisable:
		return OpenCodeProjectConfigDisable
	default:
		return OpenCodeProjectConfigInherit
	}
}

func normalizeOpenCodeDataIsolationMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case OpenCodeDataIsolationProcess:
		return OpenCodeDataIsolationProcess
	default:
		return OpenCodeDataIsolationInherit
	}
}

func normalizeOpenCodeProviderType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", OpenCodeProviderTypeOpenAICompatibleChat:
		return OpenCodeProviderTypeOpenAICompatibleChat
	case OpenCodeProviderTypeGoogleGemini:
		return OpenCodeProviderTypeGoogleGemini
	default:
		return ""
	}
}

func newOpenCodeProfileID(existing []OpenCodeAPIProfileRecord) (string, error) {
	used := map[string]struct{}{OpenCodeDefaultProfileID: {}}
	for _, record := range existing {
		if id := CanonicalOpenCodeProfileID(record.ID); id != "" {
			used[id] = struct{}{}
		}
	}
	return newRandomProfileID("opencode", "op_", used)
}
