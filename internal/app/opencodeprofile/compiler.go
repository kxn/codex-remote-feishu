package opencodeprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type CompileInput struct {
	Profile       config.OpenCodeProfile
	WorkspaceRoot string
	RuntimeDir    string
	BaseEnv       []string
}

type LaunchMaterial struct {
	Args            []string
	Env             []string
	AdmissionRef    *state.OpenCodeAdmissionRef
	RedactedSummary string
}

type configOverlay struct {
	Provider      map[string]providerOverlay `json:"provider,omitempty"`
	Model         string                     `json:"model,omitempty"`
	SmallModel    string                     `json:"small_model,omitempty"`
	ReviewModel   string                     `json:"review_model,omitempty"`
	SubagentModel string                     `json:"subagent_model,omitempty"`
	Instructions  string                     `json:"instructions,omitempty"`
	Reasoning     string                     `json:"reasoning,omitempty"`
	Permission    string                     `json:"permission,omitempty"`
}

type providerOverlay struct {
	NPM     string         `json:"npm,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

type authOverlay struct {
	Provider map[string]authProviderOverlay `json:"provider,omitempty"`
}

type authProviderOverlay struct {
	APIKey string `json:"apiKey,omitempty"`
}

func CompileLaunchMaterial(input CompileInput) (LaunchMaterial, error) {
	profile := input.Profile
	profile.ID = config.NormalizeOpenCodeProfileID(profile.ID)
	if profile.Revision == 0 {
		profile.Revision = 1
	}
	workspaceRoot := strings.TrimSpace(input.WorkspaceRoot)
	args := []string{"acp"}
	if workspaceRoot != "" {
		args = append(args, "--cwd", workspaceRoot)
	}
	env := removeOpenCodeOverlayEnv(input.BaseEnv)
	material := LaunchMaterial{
		Args: args,
		Env:  env,
		AdmissionRef: &state.OpenCodeAdmissionRef{
			ProfileRef: state.OpenCodeProfileRef{
				ID:       profile.ID,
				Revision: profile.Revision,
			},
		},
	}
	if profile.BuiltIn || profile.ID == config.OpenCodeDefaultProfileID {
		material.RedactedSummary = "opencode profile op_default inherit"
		return material, nil
	}
	if status := config.OpenCodeAPIProfileStatus(profile.OpenCodeAPIProfileSecretConfig); status != "" {
		return LaunchMaterial{}, fmt.Errorf("%s", status)
	}
	providerID := generatedProviderID(profile.ID)
	configRaw, err := json.Marshal(configOverlay{
		Provider: map[string]providerOverlay{
			providerID: {
				NPM: "@ai-sdk/openai-compatible",
				Options: map[string]any{
					"baseURL": strings.TrimSpace(profile.BaseURL),
				},
			},
		},
		Model:         providerID + "/" + strings.TrimSpace(profile.Model),
		SmallModel:    prefixedModel(providerID, profile.SmallModel),
		ReviewModel:   prefixedModel(providerID, profile.ReviewModel),
		SubagentModel: prefixedModel(providerID, profile.SubagentModel),
		Instructions:  strings.TrimSpace(profile.Instruction),
		Reasoning:     strings.TrimSpace(profile.ReasoningEffort),
		Permission:    strings.TrimSpace(profile.PermissionMode),
	})
	if err != nil {
		return LaunchMaterial{}, err
	}
	authRaw, err := json.Marshal(authOverlay{
		Provider: map[string]authProviderOverlay{
			providerID: {APIKey: profile.APIKey},
		},
	})
	if err != nil {
		return LaunchMaterial{}, err
	}
	env = config.UpsertEnvValue(env, config.OpenCodeConfigContentEnv, string(configRaw))
	env = config.UpsertEnvValue(env, config.OpenCodeAuthContentEnv, string(authRaw))
	if profile.ProjectConfigMode == config.OpenCodeProjectConfigDisable {
		env = config.UpsertEnvValue(env, config.OpenCodeDisableProjectConfigEnv, "1")
	}
	if profile.DataIsolationMode == config.OpenCodeDataIsolationProcess {
		runtimeDir := strings.TrimSpace(input.RuntimeDir)
		if runtimeDir == "" {
			return LaunchMaterial{}, fmt.Errorf("opencode data isolation requires runtime dir")
		}
		env = config.UpsertEnvValue(env, "XDG_DATA_HOME", filepath.Join(runtimeDir, "data"))
		env = config.UpsertEnvValue(env, "XDG_STATE_HOME", filepath.Join(runtimeDir, "state"))
		env = config.UpsertEnvValue(env, "XDG_CACHE_HOME", filepath.Join(runtimeDir, "cache"))
	}
	material.Env = env
	material.RedactedSummary = "opencode profile " + profile.ID + " provider=" + providerID + " apiKey=<redacted>"
	return material, nil
}

func removeOpenCodeOverlayEnv(env []string) []string {
	remove := map[string]struct{}{
		config.OpenCodeConfigContentEnv:        {},
		config.OpenCodeAuthContentEnv:          {},
		config.OpenCodeDisableProjectConfigEnv: {},
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, blocked := remove[key]; blocked {
				continue
			}
		}
		out = append(out, entry)
	}
	return out
}

func generatedProviderID(profileID string) string {
	profileID = config.NormalizeOpenCodeProfileID(profileID)
	if strings.HasPrefix(profileID, "op_") {
		return "codex_remote_opencode_" + profileID
	}
	sum := sha256.Sum256([]byte("opencode-profile-provider-v1\x00" + profileID))
	return "codex_remote_opencode_" + hex.EncodeToString(sum[:8])
}

func prefixedModel(providerID, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	return providerID + "/" + model
}
