package codexprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/codexcatalog"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	ErrorProfileRevisionUnavailable  = "profile_revision_unavailable"
	ErrorProfileDefinitionIncomplete = "profile_definition_incomplete"
	ErrorProfileSecretMissing        = "profile_secret_missing"
	ErrorOAuthMissing                = "oauth_missing"
	ErrorManagedModelCatalogMissing  = "managed_model_catalog_missing"
)

type RuntimeResolver struct {
	APIProfiles             []config.CodexAPIProfileRecord
	Preference              func(state.CodexContextPreferenceRef) (state.ProfileContextPreference, bool)
	OAuthState              *state.CodexOAuthProfileState
	Native                  state.CodexNativeConnectionEvidence
	ReservedProviderIDs     []string
	NativeProviderEnvKeys   []string
	NativeConfigProbeFailed bool
	CapabilitySet           string
	CapabilityErrorCode     string
	CapabilityErrorStage    string
	ManagedModelCatalogDir  string
}

type RuntimeProjection struct {
	Connection state.CodexConnectionContract
	Thread     state.CodexThreadPolicy
	Launch     SecretLaunchMaterial
}

type SecretLaunchMaterial struct {
	CLIOverrides   []string
	ClearedEnvKeys []string
	SecretChildEnv []string
	ManagedFiles   []LaunchManagedFile
}

type LaunchManagedFile struct {
	Path    string
	Content []byte
}

func (SecretLaunchMaterial) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("codex secret launch material cannot be serialized")
}

func ApplyLaunchMaterial(baseEnv, baseArgs []string, material SecretLaunchMaterial) ([]string, []string) {
	env := removeEnvKeys(baseEnv, material.ClearedEnvKeys)
	for _, entry := range material.SecretChildEnv {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		env = upsertRuntimeEnv(env, key, value)
	}
	args := append(append([]string{}, baseArgs...), material.CLIOverrides...)
	return env, args
}

func EnsureLaunchManagedFiles(material SecretLaunchMaterial) error {
	for _, file := range material.ManagedFiles {
		if err := ensureLaunchManagedFile(file); err != nil {
			return err
		}
	}
	return nil
}

func ensureLaunchManagedFile(file LaunchManagedFile) error {
	path := filepath.Clean(strings.TrimSpace(file.Path))
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("managed launch file path must be absolute")
	}
	if len(file.Content) == 0 {
		return fmt.Errorf("managed launch file %q has empty content", path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create managed launch file dir: %w", err)
	}
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, file.Content) {
		return nil
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create managed launch temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(file.Content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write managed launch temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod managed launch temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close managed launch temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace managed launch file: %w", err)
	}
	cleanup = false
	return nil
}

func LegacyThreadCLIOverrides(policy state.CodexThreadPolicy) []string {
	if policy.ModelMode != state.CodexThreadValueExplicit || policy.ReasoningMode != state.CodexThreadValueExplicit {
		return nil
	}
	overrides := []string{
		"-c", codexOverride("model", policy.Model),
		"-c", codexOverride("model_reasoning_effort", policy.ReasoningEffort),
	}
	if policy.ReviewModelMode == state.CodexReviewModelExplicit {
		overrides = append(overrides, "-c", codexOverride("review_model", policy.ReviewModel))
	}
	return overrides
}

type RuntimeError struct {
	Code  string
	Stage string
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Stage) != "" {
		return e.Code + ": " + e.Stage
	}
	return e.Code
}

func RuntimeErrorCode(err error) string {
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) && runtimeErr != nil {
		return runtimeErr.Code
	}
	return ""
}

func RuntimeErrorStage(err error) string {
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) && runtimeErr != nil {
		return strings.TrimSpace(runtimeErr.Stage)
	}
	return ""
}

func (r RuntimeResolver) Resolve(ref state.CodexAdmissionRef) (RuntimeProjection, error) {
	profileID := strings.TrimSpace(ref.ProfileRef.ID)
	if profileID == "" || ref.ProfileRef.Revision == 0 || ref.ContextPreferenceRef.ProfileID != profileID || ref.ContextPreferenceRef.Revision == 0 {
		return RuntimeProjection{}, runtimeError(ErrorProfileRevisionUnavailable)
	}
	capabilitySet := strings.TrimSpace(r.CapabilitySet)
	if profileID == state.OAuthCodexProfileID {
		return r.resolveOAuth(ref, capabilitySet)
	}
	if capabilitySet != CodexProfileCapabilitySetV1 {
		code := strings.TrimSpace(r.CapabilityErrorCode)
		if code == "" {
			code = ErrorCodexCapabilityUnsupported
		}
		return RuntimeProjection{}, runtimeErrorWithStage(code, r.CapabilityErrorStage)
	}
	preference, ok := r.resolvePreference(ref.ContextPreferenceRef)
	if !ok {
		return RuntimeProjection{}, runtimeError(ErrorProfileRevisionUnavailable)
	}

	var projection RuntimeProjection
	switch profileID {
	case state.NativeCodexProfileID:
		projection, ok = r.resolveNative(ref, preference, capabilitySet)
	default:
		return r.resolveAPI(ref, preference, capabilitySet)
	}
	if !ok {
		return RuntimeProjection{}, runtimeError(ErrorProfileRevisionUnavailable)
	}
	return projection, nil
}

func (r RuntimeResolver) resolvePreference(ref state.CodexContextPreferenceRef) (state.ProfileContextPreference, bool) {
	if r.Preference == nil {
		return state.ProfileContextPreference{}, false
	}
	preference, ok := r.Preference(ref)
	if !ok || preference.ProfileID != ref.ProfileID || preference.Revision != ref.Revision {
		return state.ProfileContextPreference{}, false
	}
	return preference, true
}

func (r RuntimeResolver) resolveAPI(ref state.CodexAdmissionRef, preference state.ProfileContextPreference, capabilitySet string) (RuntimeProjection, error) {
	profile, ok := findAPIProfileRevision(r.APIProfiles, ref.ProfileRef)
	if !ok {
		return RuntimeProjection{}, runtimeError(ErrorProfileRevisionUnavailable)
	}
	if status := config.CodexAPIProfileStatus(profile); status != "" {
		return RuntimeProjection{}, runtimeError(status)
	}

	providerID := availableInternalProviderID(profile.ID, r.ReservedProviderIDs)
	connection := state.CodexConnectionContract{
		ProfileRef:           ref.ProfileRef,
		ConnectionGeneration: profile.ConnectionGeneration,
		Kind:                 state.CodexProfileKindAPI,
		ModelProviderID:      providerID,
		ModelEndpointID:      profile.BaseURL,
		CapabilitySet:        capabilitySet,
	}
	connection.ConnectionContractID = connectionContractID(connection)
	thread := explicitAPIThreadPolicy(profile, preference)
	thread.ThreadPolicyID = threadPolicyID(ref, thread)
	prefix := "model_providers." + providerID
	launch := SecretLaunchMaterial{
		CLIOverrides: []string{
			"-c", codexOverride("model_provider", providerID),
			"-c", codexOverride(prefix+".name", "Codex Remote API"),
			"-c", codexOverride(prefix+".base_url", profile.BaseURL),
			"-c", codexOverride(prefix+".wire_api", "responses"),
			"-c", codexOverride(prefix+".env_key", CodexProfileAPIKeyEnv),
			"-c", prefix + ".requires_openai_auth=false",
			"-c", prefix + ".supports_websockets=false",
			"-c", codexOverride("cli_auth_credentials_store", "ephemeral"),
		},
		ClearedEnvKeys: nonNativeClearedEnvKeys(r.NativeProviderEnvKeys),
		SecretChildEnv: []string{CodexProfileAPIKeyEnv + "=" + profile.APIKey},
	}
	subagentModel := strings.TrimSpace(profile.SubagentModel)
	instruction := strings.TrimSpace(profile.Instruction)
	if subagentModel != "" {
		launch.CLIOverrides = append(launch.CLIOverrides, "-c", codexOverride("agents.default_subagent_model", subagentModel))
	}
	deepSeek := codexcatalog.IsDeepSeekProfile(profile.BaseURL, profile.Model)
	if subagentModel != "" || deepSeek || instruction != "" {
		var catalogPath string
		var catalogJSON []byte
		switch {
		case deepSeek && subagentModel == "":
			catalogPath = codexcatalog.DeepSeekModelCatalogPath(r.ManagedModelCatalogDir)
			if instruction != "" {
				catalogJSON = codexcatalog.AppendCatalogInstruction(codexcatalog.DeepSeekModelCatalogJSON(), instruction)
			} else {
				catalogJSON = codexcatalog.DeepSeekModelCatalogJSON()
			}
		default:
			if deepSeek {
				catalogPath = codexcatalog.DeepSeekModelCatalogPath(r.ManagedModelCatalogDir)
			} else {
				catalogPath = codexcatalog.ManagedModelCatalogPath(r.ManagedModelCatalogDir)
			}
			catalogJSON = codexcatalog.BuildManagedModelCatalogWithInstruction(
				[]string{profile.Model, profile.ReviewModel, profile.SubagentModel},
				instruction,
			)
		}
		if catalogPath == "" || len(catalogJSON) == 0 {
			return RuntimeProjection{}, runtimeError(ErrorManagedModelCatalogMissing)
		}
		launch.CLIOverrides = append(launch.CLIOverrides, "-c", codexOverride("model_catalog_json", catalogPath))
		launch.ManagedFiles = append(launch.ManagedFiles, LaunchManagedFile{
			Path:    catalogPath,
			Content: catalogJSON,
		})
	}
	return RuntimeProjection{Connection: connection, Thread: thread, Launch: launch}, nil
}

func (r RuntimeResolver) resolveOAuth(ref state.CodexAdmissionRef, capabilitySet string) (RuntimeProjection, error) {
	oauth := r.OAuthState
	if oauth == nil || oauth.ProfileID != state.OAuthCodexProfileID || oauth.Revision != ref.ProfileRef.Revision {
		return RuntimeProjection{}, runtimeError(ErrorProfileRevisionUnavailable)
	}
	switch oauth.Status {
	case string(OAuthProbeStatusDetected):
	case string(OAuthProbeStatusMissing):
		return RuntimeProjection{}, runtimeError(ErrorOAuthMissing)
	default:
		if code := knownOAuthProbeRuntimeError(oauth.LastProbeErrorCode); code != "" {
			return RuntimeProjection{}, runtimeError(code)
		}
		return RuntimeProjection{}, runtimeError(ErrorOAuthProbeUnknown)
	}
	if oauth.AvailabilityCode != "" {
		return RuntimeProjection{}, runtimeError(oauth.AvailabilityCode)
	}
	if capabilitySet != CodexProfileCapabilitySetV1 {
		code := strings.TrimSpace(r.CapabilityErrorCode)
		if code == "" {
			code = ErrorCodexCapabilityUnsupported
		}
		return RuntimeProjection{}, runtimeErrorWithStage(code, r.CapabilityErrorStage)
	}
	if strings.TrimSpace(oauth.CapabilitySet) != CodexProfileCapabilitySetV1 {
		return RuntimeProjection{}, runtimeError(ErrorCodexCapabilityUnsupported)
	}
	preference, ok := r.resolvePreference(ref.ContextPreferenceRef)
	if !ok {
		return RuntimeProjection{}, runtimeError(ErrorProfileRevisionUnavailable)
	}
	connection := state.CodexConnectionContract{
		ProfileRef:           ref.ProfileRef,
		ConnectionGeneration: oauth.AuthGeneration,
		Kind:                 state.CodexProfileKindOAuth,
		ModelProviderID:      "openai",
		ModelEndpointID:      "openai_chatgpt_official",
		ChatGPTEndpointID:    "chatgpt_official",
		CapabilitySet:        capabilitySet,
	}
	connection.ConnectionContractID = connectionContractID(connection)
	thread := defaultThreadPolicy(preference)
	thread.ThreadPolicyID = threadPolicyID(ref, thread)
	return RuntimeProjection{
		Connection: connection,
		Thread:     thread,
		Launch: SecretLaunchMaterial{
			CLIOverrides: []string{
				"-c", codexOverride("model_provider", "openai"),
				"-c", codexOverride("openai_base_url", ""),
			},
			ClearedEnvKeys: nonNativeClearedEnvKeys(r.NativeProviderEnvKeys),
		},
	}, nil
}

func (r RuntimeResolver) resolveNative(ref state.CodexAdmissionRef, preference state.ProfileContextPreference, capabilitySet string) (RuntimeProjection, bool) {
	if r.Native.Revision != ref.ProfileRef.Revision || r.Native.ConnectionGeneration == 0 {
		return RuntimeProjection{}, false
	}
	connection := state.CodexConnectionContract{
		ProfileRef:           ref.ProfileRef,
		ConnectionGeneration: r.Native.ConnectionGeneration,
		Kind:                 state.CodexProfileKindNative,
		ModelProviderID:      strings.TrimSpace(r.Native.ModelProviderID),
		ModelEndpointID:      strings.TrimSpace(r.Native.ModelEndpointID),
		ChatGPTEndpointID:    strings.TrimSpace(r.Native.ChatGPTEndpointID),
		CapabilitySet:        capabilitySet,
	}
	connection.ConnectionContractID = connectionContractID(connection)
	thread := defaultThreadPolicy(preference)
	thread.ThreadPolicyID = threadPolicyID(ref, thread)
	return RuntimeProjection{Connection: connection, Thread: thread}, true
}

func findAPIProfileRevision(records []config.CodexAPIProfileRecord, ref state.CodexProfileRef) (config.CodexAPIProfileSecretConfig, bool) {
	for _, record := range records {
		if record.ID != ref.ID {
			continue
		}
		for _, revision := range record.Revisions {
			if revision.ID == ref.ID && revision.Revision == ref.Revision {
				return revision, true
			}
		}
	}
	return config.CodexAPIProfileSecretConfig{}, false
}

func explicitAPIThreadPolicy(profile config.CodexAPIProfileSecretConfig, preference state.ProfileContextPreference) state.CodexThreadPolicy {
	reviewMode := state.CodexReviewModelSameAsMain
	if strings.TrimSpace(profile.ReviewModel) != "" {
		reviewMode = state.CodexReviewModelExplicit
	}
	policy := state.CodexThreadPolicy{
		ModelMode:          state.CodexThreadValueExplicit,
		Model:              profile.Model,
		ReviewModelMode:    reviewMode,
		ReviewModel:        profile.ReviewModel,
		ReasoningMode:      state.CodexThreadValueExplicit,
		ReasoningEffort:    profile.ReasoningEffort,
		ContextMode:        preference.Mode,
		PreferenceRevision: preference.Revision,
	}
	applyContextPreference(&policy)
	return policy
}

func defaultThreadPolicy(preference state.ProfileContextPreference) state.CodexThreadPolicy {
	policy := state.CodexThreadPolicy{
		ModelMode:          state.CodexThreadValueDefault,
		ReviewModelMode:    state.CodexReviewModelConfig,
		ReasoningMode:      state.CodexThreadValueDefault,
		ContextMode:        preference.Mode,
		PreferenceRevision: preference.Revision,
	}
	applyContextPreference(&policy)
	return policy
}

func applyContextPreference(policy *state.CodexThreadPolicy) {
	switch policy.ContextMode {
	case state.CodexContextModePrice272K:
		policy.ContextWindow = 272000
		policy.AutoCompactLimit = 244800
	case state.CodexContextModeExtended:
		policy.ContextWindow = 1000000
		policy.AutoCompactLimit = 900000
	default:
		policy.ContextMode = state.CodexContextModeDefault
	}
}

func internalProviderID(profileID string) string {
	return internalProviderIDWithSeed(profileID, 0)
}

func availableInternalProviderID(profileID string, reserved []string) string {
	for seed := uint64(0); ; seed++ {
		candidate := internalProviderIDWithSeed(profileID, seed)
		if !containsFold(reserved, candidate) {
			return candidate
		}
	}
}

func internalProviderIDWithSeed(profileID string, seed uint64) string {
	input := "codex-remote-profile-provider-v1\x00" + profileID
	if seed > 0 {
		input += "\x00collision:" + strconv.FormatUint(seed, 10)
	}
	sum := sha256.Sum256([]byte(input))
	return "codex_remote_profile_" + hex.EncodeToString(sum[:12])
}

func connectionContractID(contract state.CodexConnectionContract) string {
	identity := []string{
		"codex-connection-v1",
		contract.ProfileRef.ID,
		strconv.FormatUint(contract.ConnectionGeneration, 10),
		string(contract.Kind),
		contract.ModelProviderID,
		contract.ModelEndpointID,
		contract.ChatGPTEndpointID,
		contract.CapabilitySet,
	}
	return hashContract(identity)
}

func threadPolicyID(ref state.CodexAdmissionRef, policy state.CodexThreadPolicy) string {
	policy.ThreadPolicyID = ""
	raw, _ := json.Marshal(struct {
		Ref    state.CodexAdmissionRef `json:"ref"`
		Policy state.CodexThreadPolicy `json:"policy"`
	}{Ref: ref, Policy: policy})
	return hashContract([]string{"codex-thread-policy-v1", string(raw)})
}

func hashContract(parts []string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "v1:" + hex.EncodeToString(sum[:])
}

func codexOverride(key, value string) string {
	return strings.TrimSpace(key) + "=" + strconv.Quote(strings.TrimSpace(value))
}

func runtimeError(code string) error {
	return &RuntimeError{Code: code}
}

func runtimeErrorWithStage(code, stage string) error {
	return &RuntimeError{Code: code, Stage: strings.TrimSpace(stage)}
}

func knownOAuthProbeRuntimeError(code string) string {
	switch strings.TrimSpace(code) {
	case ErrorCodexCapabilityUnsupported:
		return ErrorCodexCapabilityUnsupported
	case ErrorOAuthProbeUnknown:
		return ErrorOAuthProbeUnknown
	default:
		return ""
	}
}

func upsertRuntimeEnv(env []string, key, value string) []string {
	entry := key + "=" + value
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, current := range env {
		currentKey, _, ok := strings.Cut(current, "=")
		if ok && strings.EqualFold(currentKey, key) {
			if !replaced {
				out = append(out, entry)
				replaced = true
			}
			continue
		}
		out = append(out, current)
	}
	if !replaced {
		out = append(out, entry)
	}
	return out
}

func nonNativeClearedEnvKeys(nativeProviderEnvKeys []string) []string {
	keys := ConflictingCodexAuthEnvKeys()
	for _, key := range nativeProviderEnvKeys {
		key = strings.TrimSpace(key)
		if key == "" || containsFold(keys, key) {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}
