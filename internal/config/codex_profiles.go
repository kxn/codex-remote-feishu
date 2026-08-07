package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"golang.org/x/text/cases"
)

type CodexProfileKind = state.CodexProfileKind

const (
	CodexProfileKindNative = state.CodexProfileKindNative
	CodexProfileKindOAuth  = state.CodexProfileKindOAuth
	CodexProfileKindAPI    = state.CodexProfileKindAPI

	CodexNativeProfileID = "cp_native"
	CodexOAuthProfileID  = "cp_oauth"
	CodexProfileLimit    = 50
)

type CodexAPIProfileSecretConfig struct {
	ID                   string           `json:"id"`
	Revision             uint64           `json:"revision"`
	CredentialGeneration uint64           `json:"credentialGeneration"`
	ConnectionGeneration uint64           `json:"connectionGeneration"`
	Kind                 CodexProfileKind `json:"kind"`
	Name                 string           `json:"name"`
	BaseURL              string           `json:"baseURL"`
	APIKey               string           `json:"apiKey"`
	Model                string           `json:"model,omitempty"`
	ReviewModel          string           `json:"reviewModel,omitempty"`
	SubagentModel        string           `json:"subagentModel,omitempty"`
	Instruction          string           `json:"instruction,omitempty"`
	ReasoningEffort      string           `json:"reasoningEffort,omitempty"`
}

type CodexAPIProfileRecord struct {
	ID              string                        `json:"id"`
	CurrentRevision uint64                        `json:"currentRevision"`
	Revisions       []CodexAPIProfileSecretConfig `json:"revisions"`
}

type CodexAPIProfileInput struct {
	Name            string
	BaseURL         string
	APIKey          string
	Model           string
	ReviewModel     string
	SubagentModel   string
	Instruction     string
	ReasoningEffort string
}

type CodexProfileMigrationDiagnostic struct {
	ProfileID string `json:"profileID"`
	Code      string `json:"code"`
}

func CurrentCodexAPIProfile(record CodexAPIProfileRecord) (CodexAPIProfileSecretConfig, bool) {
	for _, revision := range record.Revisions {
		if revision.Revision == record.CurrentRevision && revision.ID == record.ID {
			return revision, true
		}
	}
	return CodexAPIProfileSecretConfig{}, false
}

func CodexAPIProfileStatus(profile CodexAPIProfileSecretConfig) string {
	if strings.TrimSpace(profile.BaseURL) == "" || validateCodexAPIProfileBaseURL(strings.TrimSpace(profile.BaseURL)) != nil ||
		strings.TrimSpace(profile.Model) == "" || strings.TrimSpace(profile.ReasoningEffort) == "" {
		return "profile_definition_incomplete"
	}
	if profile.APIKey == "" {
		return "profile_secret_missing"
	}
	return ""
}

func PublicCodexAPIProfileBaseURL(profile CodexAPIProfileSecretConfig) string {
	baseURL := strings.TrimSpace(profile.BaseURL)
	if validateCodexAPIProfileBaseURL(baseURL) != nil {
		return ""
	}
	return baseURL
}

func MigrateLegacyCodexProviders(cfg AppConfig) (AppConfig, bool, []CodexProfileMigrationDiagnostic) {
	if len(cfg.Codex.Providers) == 0 || len(cfg.Codex.Profiles) != 0 {
		return cfg, false, nil
	}
	profiles := make([]CodexAPIProfileRecord, 0, len(cfg.Codex.Providers))
	diagnostics := make([]CodexProfileMigrationDiagnostic, 0)
	used := map[string]struct{}{CodexDefaultProviderID: {}}
	for _, provider := range cfg.Codex.Providers {
		id := nextCodexProviderID(provider.ID, provider.Name, used)
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = id
		}
		profile := CodexAPIProfileSecretConfig{
			ID:                   id,
			Revision:             1,
			CredentialGeneration: 1,
			ConnectionGeneration: 1,
			Kind:                 CodexProfileKindAPI,
			Name:                 name,
			BaseURL:              strings.TrimSpace(provider.BaseURL),
			APIKey:               provider.APIKey,
			Model:                strings.TrimSpace(provider.Model),
			ReviewModel:          "",
			ReasoningEffort:      strings.TrimSpace(provider.ReasoningEffort),
		}
		profiles = append(profiles, CodexAPIProfileRecord{
			ID:              id,
			CurrentRevision: 1,
			Revisions:       []CodexAPIProfileSecretConfig{profile},
		})
		if code := CodexAPIProfileStatus(profile); code != "" {
			diagnostics = append(diagnostics, CodexProfileMigrationDiagnostic{ProfileID: id, Code: code})
		}
	}
	cfg.Codex.Profiles = profiles
	cfg.Codex.Providers = nil
	return cfg, true, diagnostics
}

func PrepareCodexAPIProfileCreate(existing []CodexAPIProfileRecord, input CodexAPIProfileInput) (CodexAPIProfileRecord, error) {
	if len(existing) >= CodexProfileLimit {
		return CodexAPIProfileRecord{}, fmt.Errorf("codex profile catalog limit reached")
	}
	input, err := validateCodexAPIProfileInput(input, true)
	if err != nil {
		return CodexAPIProfileRecord{}, err
	}
	if err := validateCodexProfileNameUnique(existing, "", input.Name); err != nil {
		return CodexAPIProfileRecord{}, err
	}
	id, err := newCodexProfileID(existing)
	if err != nil {
		return CodexAPIProfileRecord{}, err
	}
	profile := CodexAPIProfileSecretConfig{
		ID:                   id,
		Revision:             1,
		CredentialGeneration: 1,
		ConnectionGeneration: 1,
		Kind:                 CodexProfileKindAPI,
		Name:                 input.Name,
		BaseURL:              input.BaseURL,
		APIKey:               input.APIKey,
		Model:                input.Model,
		ReviewModel:          input.ReviewModel,
		SubagentModel:        input.SubagentModel,
		Instruction:          input.Instruction,
		ReasoningEffort:      input.ReasoningEffort,
	}
	return CodexAPIProfileRecord{ID: id, CurrentRevision: 1, Revisions: []CodexAPIProfileSecretConfig{profile}}, nil
}

func PrepareCodexAPIProfileUpdate(record CodexAPIProfileRecord, input CodexAPIProfileInput) (CodexAPIProfileRecord, bool, error) {
	current, ok := CurrentCodexAPIProfile(record)
	if !ok {
		return record, false, fmt.Errorf("codex profile current revision is missing")
	}
	if input.APIKey == "" {
		input.APIKey = current.APIKey
	}
	input, err := validateCodexAPIProfileInput(input, true)
	if err != nil {
		return record, false, err
	}
	if current.Name == input.Name && current.BaseURL == input.BaseURL && current.APIKey == input.APIKey &&
		current.Model == input.Model && current.ReviewModel == input.ReviewModel && current.SubagentModel == input.SubagentModel &&
		current.Instruction == input.Instruction && current.ReasoningEffort == input.ReasoningEffort {
		return record, false, nil
	}
	next := current
	next.Revision = current.Revision + 1
	next.Name = input.Name
	next.BaseURL = input.BaseURL
	next.Model = input.Model
	next.ReviewModel = input.ReviewModel
	next.SubagentModel = input.SubagentModel
	next.Instruction = input.Instruction
	next.ReasoningEffort = input.ReasoningEffort
	if current.APIKey != input.APIKey {
		next.APIKey = input.APIKey
		next.CredentialGeneration++
	}
	if current.BaseURL != input.BaseURL || current.APIKey != input.APIKey {
		next.ConnectionGeneration++
	}
	record.CurrentRevision = next.Revision
	record.Revisions = append(append([]CodexAPIProfileSecretConfig{}, record.Revisions...), next)
	return record, true, nil
}

func IndexOfCodexAPIProfile(records []CodexAPIProfileRecord, profileID string) int {
	profileID = strings.TrimSpace(profileID)
	for index, record := range records {
		if strings.TrimSpace(record.ID) == profileID {
			return index
		}
	}
	return -1
}

func ValidateCodexAPIProfileNameUnique(records []CodexAPIProfileRecord, exceptID, name string) error {
	return validateCodexProfileNameUnique(records, strings.TrimSpace(exceptID), name)
}

func NormalizeCodexAPIProfileRecords(records []CodexAPIProfileRecord) []CodexAPIProfileRecord {
	if len(records) == 0 {
		return nil
	}
	result := make([]CodexAPIProfileRecord, 0, len(records))
	for _, record := range records {
		record.ID = strings.TrimSpace(record.ID)
		if record.ID == "" || record.CurrentRevision == 0 {
			continue
		}
		revisions := make([]CodexAPIProfileSecretConfig, 0, len(record.Revisions))
		for _, revision := range record.Revisions {
			if strings.TrimSpace(revision.ID) != record.ID || revision.Revision == 0 || revision.Kind != CodexProfileKindAPI {
				continue
			}
			revision.ID = record.ID
			revision.Name = strings.TrimSpace(revision.Name)
			revision.BaseURL = strings.TrimSpace(revision.BaseURL)
			revision.Model = strings.TrimSpace(revision.Model)
			revision.ReviewModel = strings.TrimSpace(revision.ReviewModel)
			revision.SubagentModel = strings.TrimSpace(revision.SubagentModel)
			revision.Instruction = strings.TrimSpace(revision.Instruction)
			revision.ReasoningEffort = strings.TrimSpace(revision.ReasoningEffort)
			revisions = append(revisions, revision)
		}
		record.Revisions = revisions
		if _, ok := CurrentCodexAPIProfile(record); ok {
			result = append(result, record)
		}
	}
	return result
}

func ValidateCodexAPIProfileRecords(records []CodexAPIProfileRecord) error {
	if len(records) > CodexProfileLimit {
		return fmt.Errorf("codex profile catalog limit exceeded")
	}
	ids := make(map[string]struct{}, len(records))
	names := make([]string, 0, len(records))
	for _, record := range records {
		record.ID = strings.TrimSpace(record.ID)
		if record.ID == "" || record.CurrentRevision == 0 || len(record.Revisions) == 0 {
			return fmt.Errorf("invalid codex profile record")
		}
		if _, exists := ids[record.ID]; exists {
			return fmt.Errorf("duplicate codex profile id %q", record.ID)
		}
		ids[record.ID] = struct{}{}
		revisions := make(map[uint64]struct{}, len(record.Revisions))
		var current CodexAPIProfileSecretConfig
		maxRevision := uint64(0)
		for _, revision := range record.Revisions {
			if strings.TrimSpace(revision.ID) != record.ID || revision.Revision == 0 || revision.Kind != CodexProfileKindAPI ||
				revision.CredentialGeneration == 0 || revision.ConnectionGeneration == 0 {
				return fmt.Errorf("invalid codex profile revision for %q", record.ID)
			}
			if err := validateStoredCodexAPIProfileRevision(revision); err != nil {
				return fmt.Errorf("invalid codex profile revision for %q: %w", record.ID, err)
			}
			if _, exists := revisions[revision.Revision]; exists {
				return fmt.Errorf("duplicate codex profile revision for %q", record.ID)
			}
			revisions[revision.Revision] = struct{}{}
			if revision.Revision > maxRevision {
				maxRevision = revision.Revision
			}
			if revision.Revision == record.CurrentRevision {
				current = revision
			}
		}
		if err := validateCodexAPIProfileGenerations(record.Revisions); err != nil {
			return fmt.Errorf("invalid codex profile generations for %q: %w", record.ID, err)
		}
		if current.Revision == 0 || maxRevision != record.CurrentRevision {
			return fmt.Errorf("missing or stale current codex profile revision for %q", record.ID)
		}
		for _, name := range names {
			if codexProfileNameKey(name) == codexProfileNameKey(current.Name) {
				return fmt.Errorf("duplicate codex profile name")
			}
		}
		names = append(names, current.Name)
	}
	return nil
}

func PruneCodexAPIProfileHistory(record CodexAPIProfileRecord, retained map[uint64]struct{}) CodexAPIProfileRecord {
	revisions := make([]CodexAPIProfileSecretConfig, 0, len(record.Revisions))
	for _, revision := range record.Revisions {
		_, keep := retained[revision.Revision]
		if revision.Revision == record.CurrentRevision || keep {
			revisions = append(revisions, revision)
		}
	}
	record.Revisions = revisions
	return record
}

func validateCodexProfileNameUnique(records []CodexAPIProfileRecord, exceptID, name string) error {
	nameKey := codexProfileNameKey(name)
	for _, record := range records {
		if record.ID == exceptID {
			continue
		}
		current, ok := CurrentCodexAPIProfile(record)
		if ok && codexProfileNameKey(current.Name) == nameKey {
			return fmt.Errorf("codex profile name already exists")
		}
	}
	return nil
}

func codexProfileNameKey(name string) string {
	return cases.Fold().String(strings.TrimSpace(name))
}

func validateCodexAPIProfileInput(input CodexAPIProfileInput, requireKey bool) (CodexAPIProfileInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.Model = strings.TrimSpace(input.Model)
	input.ReviewModel = strings.TrimSpace(input.ReviewModel)
	input.SubagentModel = strings.TrimSpace(input.SubagentModel)
	input.Instruction = strings.TrimSpace(input.Instruction)
	input.ReasoningEffort = strings.TrimSpace(input.ReasoningEffort)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 64 || hasUnsafeProfileText(input.Name) {
		return input, fmt.Errorf("codex profile name is invalid")
	}
	if input.BaseURL == "" || len(input.BaseURL) > 2048 {
		return input, fmt.Errorf("codex profile baseURL is invalid")
	}
	if err := validateCodexAPIProfileBaseURL(input.BaseURL); err != nil {
		return input, fmt.Errorf("codex profile baseURL is invalid: %w", err)
	}
	if input.Model == "" || len(input.Model) > 256 || hasUnsafeProfileText(input.Model) {
		return input, fmt.Errorf("codex profile model is invalid")
	}
	if len(input.ReviewModel) > 256 || hasUnsafeProfileText(input.ReviewModel) {
		return input, fmt.Errorf("codex profile reviewModel is invalid")
	}
	if len(input.SubagentModel) > 256 || hasUnsafeProfileText(input.SubagentModel) {
		return input, fmt.Errorf("codex profile subagentModel is invalid")
	}
	if err := ValidateInstruction(input.Instruction); err != nil {
		return input, fmt.Errorf("codex profile instruction is invalid: %w", err)
	}
	if input.ReasoningEffort == "" || len(input.ReasoningEffort) > 64 || hasUnsafeProfileText(input.ReasoningEffort) {
		return input, fmt.Errorf("codex profile reasoningEffort is invalid")
	}
	if requireKey && input.APIKey == "" {
		return input, fmt.Errorf("codex profile apiKey is required")
	}
	if len(input.APIKey) > 16*1024 || strings.ContainsAny(input.APIKey, "\x00\r\n") {
		return input, fmt.Errorf("codex profile apiKey is invalid")
	}
	return input, nil
}

func validateStoredCodexAPIProfileRevision(revision CodexAPIProfileSecretConfig) error {
	name := strings.TrimSpace(revision.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 || hasUnsafeProfileText(revision.Name) {
		return fmt.Errorf("invalid name")
	}
	if len(revision.BaseURL) > 2048 || hasUnsafeProfileText(revision.BaseURL) ||
		len(revision.Model) > 256 || hasUnsafeProfileText(revision.Model) ||
		len(revision.ReviewModel) > 256 || hasUnsafeProfileText(revision.ReviewModel) ||
		len(revision.SubagentModel) > 256 || hasUnsafeProfileText(revision.SubagentModel) ||
		ValidateInstruction(revision.Instruction) != nil ||
		len(revision.ReasoningEffort) > 64 || hasUnsafeProfileText(revision.ReasoningEffort) ||
		len(revision.APIKey) > 16*1024 || strings.ContainsAny(revision.APIKey, "\x00\r\n") {
		return fmt.Errorf("invalid field")
	}
	if revision.CredentialGeneration > revision.Revision || revision.ConnectionGeneration > revision.Revision ||
		revision.ConnectionGeneration < revision.CredentialGeneration {
		return fmt.Errorf("invalid generation")
	}
	return nil
}

func validateCodexAPIProfileGenerations(revisions []CodexAPIProfileSecretConfig) error {
	ordered := append([]CodexAPIProfileSecretConfig{}, revisions...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Revision < ordered[right].Revision })
	for index := 1; index < len(ordered); index++ {
		previous := ordered[index-1]
		current := ordered[index]
		revisionDelta := current.Revision - previous.Revision
		if current.CredentialGeneration < previous.CredentialGeneration ||
			current.CredentialGeneration-previous.CredentialGeneration > revisionDelta ||
			current.ConnectionGeneration < previous.ConnectionGeneration ||
			current.ConnectionGeneration-previous.ConnectionGeneration > revisionDelta {
			return fmt.Errorf("generation is not monotonic")
		}
		credentialChanged := current.CredentialGeneration != previous.CredentialGeneration
		if current.APIKey != previous.APIKey && !credentialChanged {
			return fmt.Errorf("credential changed without generation")
		}
		if (current.BaseURL != previous.BaseURL || credentialChanged) && current.ConnectionGeneration == previous.ConnectionGeneration {
			return fmt.Errorf("connection changed without generation")
		}
	}
	return nil
}

func validateCodexAPIProfileBaseURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return fmt.Errorf("absolute http(s) URL without userinfo, query, or fragment is required")
	}
	return nil
}

func hasUnsafeProfileText(value string) bool {
	for _, current := range value {
		if current == '\n' || current == '\r' || unicode.IsControl(current) {
			return true
		}
	}
	return false
}

func newCodexProfileID(existing []CodexAPIProfileRecord) (string, error) {
	used := map[string]struct{}{CodexNativeProfileID: {}, CodexOAuthProfileID: {}}
	for _, record := range existing {
		used[record.ID] = struct{}{}
	}
	for attempt := 0; attempt < 4; attempt++ {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return "", fmt.Errorf("generate codex profile id: %w", err)
		}
		candidate := "cp_" + hex.EncodeToString(raw)
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("generate unique codex profile id")
}
