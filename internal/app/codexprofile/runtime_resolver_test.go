package codexprofile

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRuntimeResolverProjectsAPIConnectionThreadAndSecretSeparately(t *testing.T) {
	profile := config.CodexAPIProfileSecretConfig{
		ID:                   "cp_api_a",
		Revision:             3,
		CredentialGeneration: 2,
		ConnectionGeneration: 2,
		Kind:                 state.CodexProfileKindAPI,
		Name:                 "Team API",
		BaseURL:              "https://api.example.com/v1",
		APIKey:               "secret-a",
		Model:                "gpt-5.6",
		ReviewModel:          "gpt-5.6-mini",
		ReasoningEffort:      "high",
	}
	resolver := RuntimeResolver{
		APIProfiles: []config.CodexAPIProfileRecord{{
			ID: "cp_api_a", CurrentRevision: 3, Revisions: []config.CodexAPIProfileSecretConfig{profile},
		}},
		Preference: fixedPreferenceLookup(state.ProfileContextPreference{
			ProfileID: "cp_api_a", Revision: 4, Mode: state.CodexContextModePrice272K,
		}),
		CapabilitySet: CodexProfileCapabilitySetV1,
	}

	projection, err := resolver.Resolve(state.CodexAdmissionRef{
		ProfileRef:           state.CodexProfileRef{ID: "cp_api_a", Revision: 3},
		ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: "cp_api_a", Revision: 4},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if projection.Connection.Kind != state.CodexProfileKindAPI || projection.Connection.ProfileRef.Revision != 3 {
		t.Fatalf("unexpected connection: %#v", projection.Connection)
	}
	if !strings.HasPrefix(projection.Connection.ModelProviderID, "codex_remote_profile_") {
		t.Fatalf("internal provider id = %q", projection.Connection.ModelProviderID)
	}
	if projection.Connection.ModelProviderID == profile.ID || projection.Connection.ModelEndpointID != profile.BaseURL {
		t.Fatalf("connection leaked user profile id or lost endpoint: %#v", projection.Connection)
	}
	if projection.Thread.ModelMode != state.CodexThreadValueExplicit || projection.Thread.Model != profile.Model ||
		projection.Thread.ReviewModelMode != state.CodexReviewModelExplicit || projection.Thread.ReasoningEffort != "high" {
		t.Fatalf("unexpected thread policy: %#v", projection.Thread)
	}
	if projection.Thread.ContextWindow != 272000 || projection.Thread.AutoCompactLimit != 244800 {
		t.Fatalf("unexpected context projection: %#v", projection.Thread)
	}
	if lookupProbeTestEnv(projection.Launch.SecretChildEnv, CodexProfileAPIKeyEnv) != "secret-a" {
		t.Fatalf("secret launch env missing profile key: %#v", projection.Launch.SecretChildEnv)
	}
	joinedArgs := strings.Join(projection.Launch.CLIOverrides, "\n")
	for _, required := range []string{
		`model_provider="` + projection.Connection.ModelProviderID + `"`,
		`.wire_api="responses"`,
		`.env_key="` + CodexProfileAPIKeyEnv + `"`,
		`.requires_openai_auth=false`,
		`.supports_websockets=false`,
		`cli_auth_credentials_store="ephemeral"`,
	} {
		if !strings.Contains(joinedArgs, required) {
			t.Fatalf("launch overrides missing %q: %#v", required, projection.Launch.CLIOverrides)
		}
	}
	if strings.Contains(joinedArgs, profile.Model) || strings.Contains(joinedArgs, profile.ReasoningEffort) || strings.Contains(joinedArgs, profile.ReviewModel) {
		t.Fatalf("connection launch overrides contain thread policy: %#v", projection.Launch.CLIOverrides)
	}

	publicRaw, err := json.Marshal(struct {
		Connection state.CodexConnectionContract `json:"connection"`
		Thread     state.CodexThreadPolicy       `json:"thread"`
	}{projection.Connection, projection.Thread})
	if err != nil {
		t.Fatalf("marshal public contracts: %v", err)
	}
	if strings.Contains(string(publicRaw), profile.APIKey) {
		t.Fatalf("public contracts leaked API key: %s", publicRaw)
	}
	if _, err := json.Marshal(projection.Launch); err == nil {
		t.Fatal("secret launch material must reject JSON serialization")
	}
}

func TestRuntimeResolverProjectsDeepSeekManagedModelCatalog(t *testing.T) {
	profile := config.CodexAPIProfileSecretConfig{
		ID:                   "cp_deepseek",
		Revision:             1,
		CredentialGeneration: 1,
		ConnectionGeneration: 1,
		Kind:                 state.CodexProfileKindAPI,
		Name:                 "DeepSeek",
		BaseURL:              "https://api.deepseek.com/",
		APIKey:               "deepseek-secret",
		Model:                "deepseek-v4-flash",
		ReasoningEffort:      "high",
	}
	managedDir := filepath.Join(t.TempDir(), "catalogs")
	resolver := RuntimeResolver{
		APIProfiles: []config.CodexAPIProfileRecord{{
			ID: profile.ID, CurrentRevision: 1, Revisions: []config.CodexAPIProfileSecretConfig{profile},
		}},
		Preference:             fixedPreferenceLookup(state.ProfileContextPreference{ProfileID: profile.ID, Revision: 1, Mode: state.CodexContextModeDefault}),
		CapabilitySet:          CodexProfileCapabilitySetV1,
		ManagedModelCatalogDir: managedDir,
	}

	projection, err := resolver.Resolve(admissionRef(profile.ID, 1, 1))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	catalogPath := filepath.Join(managedDir, "deepseek-models-v1.json")
	if !strings.Contains(strings.Join(projection.Launch.CLIOverrides, "\n"), `model_catalog_json="`+catalogPath+`"`) {
		t.Fatalf("launch overrides missing managed catalog path: %#v", projection.Launch.CLIOverrides)
	}
	if len(projection.Launch.ManagedFiles) != 1 || projection.Launch.ManagedFiles[0].Path != catalogPath {
		t.Fatalf("expected one managed DeepSeek catalog file, got %#v", projection.Launch.ManagedFiles)
	}
	content := string(projection.Launch.ManagedFiles[0].Content)
	for _, want := range []string{"deepseek-v4-flash", "deepseek-v4-pro", `"effort": "max"`} {
		if !strings.Contains(content, want) {
			t.Fatalf("managed DeepSeek catalog missing %q: %s", want, content)
		}
	}
	if strings.Contains(content, profile.APIKey) {
		t.Fatal("managed DeepSeek catalog leaked API key")
	}
}

func TestRuntimeResolverRejectsDeepSeekWithoutManagedModelCatalogDir(t *testing.T) {
	profile := config.CodexAPIProfileSecretConfig{
		ID:                   "cp_deepseek",
		Revision:             1,
		CredentialGeneration: 1,
		ConnectionGeneration: 1,
		Kind:                 state.CodexProfileKindAPI,
		Name:                 "DeepSeek",
		BaseURL:              "https://api.deepseek.com/",
		APIKey:               "deepseek-secret",
		Model:                "deepseek-v4-flash",
		ReasoningEffort:      "high",
	}
	resolver := RuntimeResolver{
		APIProfiles: []config.CodexAPIProfileRecord{{
			ID: profile.ID, CurrentRevision: 1, Revisions: []config.CodexAPIProfileSecretConfig{profile},
		}},
		Preference:    fixedPreferenceLookup(state.ProfileContextPreference{ProfileID: profile.ID, Revision: 1, Mode: state.CodexContextModeDefault}),
		CapabilitySet: CodexProfileCapabilitySetV1,
	}

	_, err := resolver.Resolve(admissionRef(profile.ID, 1, 1))
	if got := RuntimeErrorCode(err); got != ErrorManagedModelCatalogMissing {
		t.Fatalf("error code = %q, want %q (err=%v)", got, ErrorManagedModelCatalogMissing, err)
	}
}

func TestRuntimeResolverPreservesStableAPIProfileErrors(t *testing.T) {
	tests := []struct {
		name    string
		profile config.CodexAPIProfileSecretConfig
		want    string
	}{
		{
			name: "incomplete definition",
			profile: config.CodexAPIProfileSecretConfig{
				ID: "cp_api", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
				Kind: state.CodexProfileKindAPI, BaseURL: "https://api.example/v1", APIKey: "secret",
			},
			want: ErrorProfileDefinitionIncomplete,
		},
		{
			name: "missing secret",
			profile: config.CodexAPIProfileSecretConfig{
				ID: "cp_api", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
				Kind: state.CodexProfileKindAPI, BaseURL: "https://api.example/v1", Model: "model", ReasoningEffort: "high",
			},
			want: ErrorProfileSecretMissing,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := RuntimeResolver{
				APIProfiles: []config.CodexAPIProfileRecord{{ID: "cp_api", CurrentRevision: 1, Revisions: []config.CodexAPIProfileSecretConfig{test.profile}}},
				Preference: fixedPreferenceLookup(state.ProfileContextPreference{
					ProfileID: "cp_api", Revision: 1, Mode: state.CodexContextModeDefault,
				}),
				CapabilitySet: CodexProfileCapabilitySetV1,
			}
			_, err := resolver.Resolve(admissionRef("cp_api", 1, 1))
			if got := RuntimeErrorCode(err); got != test.want {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.want, err)
			}
		})
	}
}

func TestRuntimeResolverKeepsConnectionStableAcrossThreadOnlyProfileEdit(t *testing.T) {
	base := config.CodexAPIProfileSecretConfig{
		ID: "cp_api", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
		Kind: state.CodexProfileKindAPI, Name: "API", BaseURL: "https://api.example/v1", APIKey: "secret",
		Model: "model-a", ReasoningEffort: "medium",
	}
	edited := base
	edited.Revision = 2
	edited.Model = "model-b"
	edited.ReasoningEffort = "high"
	resolver := RuntimeResolver{
		APIProfiles: []config.CodexAPIProfileRecord{{
			ID: "cp_api", CurrentRevision: 2, Revisions: []config.CodexAPIProfileSecretConfig{base, edited},
		}},
		Preference:    fixedPreferenceLookup(state.ProfileContextPreference{ProfileID: "cp_api", Revision: 1, Mode: state.CodexContextModeDefault}),
		CapabilitySet: CodexProfileCapabilitySetV1,
	}

	first, err := resolver.Resolve(admissionRef("cp_api", 1, 1))
	if err != nil {
		t.Fatalf("resolve first: %v", err)
	}
	second, err := resolver.Resolve(admissionRef("cp_api", 2, 1))
	if err != nil {
		t.Fatalf("resolve second: %v", err)
	}
	if first.Connection.ConnectionContractID != second.Connection.ConnectionContractID {
		t.Fatalf("thread-only edit changed connection id: %q != %q", first.Connection.ConnectionContractID, second.Connection.ConnectionContractID)
	}
	if first.Thread.ThreadPolicyID == second.Thread.ThreadPolicyID {
		t.Fatal("thread-only edit did not change thread policy id")
	}
}

func TestRuntimeResolverAvoidsNativeProviderIDCollision(t *testing.T) {
	profile := config.CodexAPIProfileSecretConfig{
		ID: "cp_api", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
		Kind: state.CodexProfileKindAPI, BaseURL: "https://api.example/v1", APIKey: "secret", Model: "model", ReasoningEffort: "high",
	}
	collidingID := internalProviderID(profile.ID)
	resolver := RuntimeResolver{
		APIProfiles:         []config.CodexAPIProfileRecord{{ID: profile.ID, CurrentRevision: 1, Revisions: []config.CodexAPIProfileSecretConfig{profile}}},
		Preference:          fixedPreferenceLookup(state.ProfileContextPreference{ProfileID: profile.ID, Revision: 1, Mode: state.CodexContextModeDefault}),
		CapabilitySet:       CodexProfileCapabilitySetV1,
		ReservedProviderIDs: []string{collidingID},
	}
	projection, err := resolver.Resolve(admissionRef(profile.ID, 1, 1))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if projection.Connection.ModelProviderID == collidingID {
		t.Fatalf("resolver reused colliding provider ID %q", collidingID)
	}
}

func TestRuntimeResolverFailsWhenExactRevisionIsUnavailable(t *testing.T) {
	resolver := RuntimeResolver{
		APIProfiles: []config.CodexAPIProfileRecord{{
			ID: "cp_api", CurrentRevision: 2, Revisions: []config.CodexAPIProfileSecretConfig{{
				ID: "cp_api", Revision: 2, CredentialGeneration: 1, ConnectionGeneration: 1,
				Kind: state.CodexProfileKindAPI, BaseURL: "https://api.example/v1", APIKey: "secret", Model: "model", ReasoningEffort: "high",
			}},
		}},
		Preference:    fixedPreferenceLookup(state.ProfileContextPreference{ProfileID: "cp_api", Revision: 1, Mode: state.CodexContextModeDefault}),
		CapabilitySet: CodexProfileCapabilitySetV1,
	}

	_, err := resolver.Resolve(admissionRef("cp_api", 1, 1))
	if got := RuntimeErrorCode(err); got != ErrorProfileRevisionUnavailable {
		t.Fatalf("error code = %q, want %q (err=%v)", got, ErrorProfileRevisionUnavailable, err)
	}
}

func TestRuntimeResolverRejectsUnknownCapabilitySet(t *testing.T) {
	profile := config.CodexAPIProfileSecretConfig{
		ID: "cp_api", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
		Kind: state.CodexProfileKindAPI, BaseURL: "https://api.example/v1", APIKey: "secret", Model: "model", ReasoningEffort: "high",
	}
	resolver := RuntimeResolver{
		APIProfiles:   []config.CodexAPIProfileRecord{{ID: profile.ID, CurrentRevision: 1, Revisions: []config.CodexAPIProfileSecretConfig{profile}}},
		Preference:    fixedPreferenceLookup(state.ProfileContextPreference{ProfileID: profile.ID, Revision: 1, Mode: state.CodexContextModeDefault}),
		CapabilitySet: "future-unknown-capability-set",
	}
	_, err := resolver.Resolve(admissionRef(profile.ID, 1, 1))
	if got := RuntimeErrorCode(err); got != ErrorCodexCapabilityUnsupported {
		t.Fatalf("error code = %q, want %q (err=%v)", got, ErrorCodexCapabilityUnsupported, err)
	}
}

func TestRuntimeResolverProjectsOAuthAndNativeIsolation(t *testing.T) {
	preferences := map[string]state.ProfileContextPreference{
		state.NativeCodexProfileID: {ProfileID: state.NativeCodexProfileID, Revision: 1, Mode: state.CodexContextModeDefault},
		state.OAuthCodexProfileID:  {ProfileID: state.OAuthCodexProfileID, Revision: 2, Mode: state.CodexContextModeExtended},
	}
	resolver := RuntimeResolver{
		Preference: func(ref state.CodexContextPreferenceRef) (state.ProfileContextPreference, bool) {
			preference, ok := preferences[ref.ProfileID]
			return preference, ok && preference.Revision == ref.Revision
		},
		OAuthState: &state.CodexOAuthProfileState{
			ProfileID: state.OAuthCodexProfileID, Revision: 5, AuthGeneration: 3,
			Status: string(OAuthProbeStatusDetected), CapabilitySet: CodexProfileCapabilitySetV1,
		},
		Native: state.CodexNativeConnectionEvidence{
			Revision: 1, ConnectionGeneration: 7, ModelProviderID: "native-provider",
			ModelEndpointID: "native-endpoint-generation-7", ChatGPTEndpointID: "native-chatgpt-generation-7",
		},
		CapabilitySet: CodexProfileCapabilitySetV1,
	}

	oauth, err := resolver.Resolve(admissionRef(state.OAuthCodexProfileID, 5, 2))
	if err != nil {
		t.Fatalf("resolve oauth: %v", err)
	}
	if oauth.Connection.ModelProviderID != "openai" || oauth.Thread.ModelMode != state.CodexThreadValueDefault {
		t.Fatalf("unexpected oauth projection: %#v", oauth)
	}
	if len(oauth.Launch.SecretChildEnv) != 0 || len(oauth.Launch.ClearedEnvKeys) == 0 {
		t.Fatalf("oauth launch did not clear auth environment: %#v", oauth.Launch)
	}
	if !strings.Contains(strings.Join(oauth.Launch.CLIOverrides, "\n"), `openai_base_url=""`) {
		t.Fatalf("oauth launch did not clear openai_base_url: %#v", oauth.Launch.CLIOverrides)
	}

	native, err := resolver.Resolve(admissionRef(state.NativeCodexProfileID, 1, 1))
	if err != nil {
		t.Fatalf("resolve native: %v", err)
	}
	if native.Connection.ModelProviderID != "native-provider" || len(native.Launch.CLIOverrides) != 0 || len(native.Launch.ClearedEnvKeys) != 0 {
		t.Fatalf("native projection must preserve user runtime: %#v", native)
	}
}

func TestRuntimeResolverReportsOAuthAvailabilityBeforePreferenceOrCapability(t *testing.T) {
	tests := []struct {
		name               string
		status             OAuthProbeStatus
		capability         string
		lastProbeErrorCode string
		want               string
	}{
		{name: "missing without preference", status: OAuthProbeStatusMissing, capability: CodexProfileCapabilitySetV1, want: ErrorOAuthMissing},
		{name: "unknown without capability", status: OAuthProbeStatusUnknown, want: ErrorOAuthProbeUnknown},
		{name: "unknown capability error", status: OAuthProbeStatusUnknown, lastProbeErrorCode: ErrorCodexCapabilityUnsupported, want: ErrorCodexCapabilityUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := RuntimeResolver{
				OAuthState: &state.CodexOAuthProfileState{
					ProfileID:          state.OAuthCodexProfileID,
					Revision:           1,
					Status:             string(test.status),
					LastProbeErrorCode: test.lastProbeErrorCode,
					CapabilitySet:      test.capability,
				},
				CapabilitySet: test.capability,
			}

			_, err := resolver.Resolve(admissionRef(state.OAuthCodexProfileID, 1, 1))
			if got := RuntimeErrorCode(err); got != test.want {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.want, err)
			}
		})
	}
}

func fixedPreferenceLookup(preference state.ProfileContextPreference) func(state.CodexContextPreferenceRef) (state.ProfileContextPreference, bool) {
	return func(ref state.CodexContextPreferenceRef) (state.ProfileContextPreference, bool) {
		return preference, ref.ProfileID == preference.ProfileID && ref.Revision == preference.Revision
	}
}

func admissionRef(profileID string, profileRevision, preferenceRevision uint64) state.CodexAdmissionRef {
	return state.CodexAdmissionRef{
		ProfileRef:           state.CodexProfileRef{ID: profileID, Revision: profileRevision},
		ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: profileID, Revision: preferenceRevision},
	}
}
