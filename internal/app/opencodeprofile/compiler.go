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
	Provider     map[string]providerOverlay `json:"provider,omitempty"`
	Model        string                     `json:"model,omitempty"`
	SmallModel   string                     `json:"small_model,omitempty"`
	Agent        map[string]agentOverlay    `json:"agent,omitempty"`
	Instructions string                     `json:"instructions,omitempty"`
	Permission   map[string]string          `json:"permission,omitempty"`
}

type providerOverlay struct {
	Name    string                  `json:"name,omitempty"`
	ID      string                  `json:"id,omitempty"`
	Env     []string                `json:"env,omitempty"`
	NPM     string                  `json:"npm,omitempty"`
	Models  map[string]modelOverlay `json:"models,omitempty"`
	Options map[string]any          `json:"options,omitempty"`
}

type modelOverlay struct {
	ID          string                    `json:"id,omitempty"`
	Name        string                    `json:"name,omitempty"`
	Attachment  bool                      `json:"attachment"`
	Reasoning   bool                      `json:"reasoning"`
	Temperature bool                      `json:"temperature"`
	ToolCall    bool                      `json:"tool_call"`
	ReleaseDate string                    `json:"release_date,omitempty"`
	Limit       map[string]int            `json:"limit,omitempty"`
	Cost        map[string]float64        `json:"cost,omitempty"`
	Variants    map[string]map[string]any `json:"variants,omitempty"`
	Options     map[string]any            `json:"options,omitempty"`
}

type agentOverlay struct {
	Model string `json:"model,omitempty"`
}

type authOverlay map[string]authProviderOverlay

type authProviderOverlay struct {
	Type string `json:"type"`
	Key  string `json:"key"`
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
		overlay := configOverlay{
			Model:      systemOpenCodeRecentModelForACP(env),
			Permission: openCodePermissionMode(input.RuntimeAccessMode),
		}
		if overlay.Model != "" || len(overlay.Permission) > 0 {
			configRaw, err := json.Marshal(overlay)
			if err != nil {
				return LaunchMaterial{}, err
			}
			material.Env = config.UpsertEnvValue(material.Env, config.OpenCodeConfigContentEnv, string(configRaw))
			material.RedactedSummary = "opencode profile op_default inherit"
			if overlay.Model != "" {
				material.RedactedSummary += " model=" + overlay.Model
			}
			if accessMode := state.NormalizeOpenCodeRuntimeAccessMode(input.RuntimeAccessMode); accessMode != "" {
				material.RedactedSummary += " access=" + accessMode
			}
			return material, nil
		}
		material.RedactedSummary = "opencode profile op_default inherit"
		return material, nil
	}
	if status := config.OpenCodeAPIProfileStatus(profile.OpenCodeAPIProfileSecretConfig); status != "" {
		return LaunchMaterial{}, fmt.Errorf("%s", status)
	}
	providerID := generatedProviderID(profile.ID)
	models := make(map[string]modelOverlay)
	addModelOverlay(models, profile.Model, profile.ReasoningEffort)
	addModelOverlay(models, profile.SmallModel, "")
	addModelOverlay(models, profile.SubagentModel, "")
	configRaw, err := json.Marshal(configOverlay{
		Provider: map[string]providerOverlay{
			providerID: {
				Name:   "Codex Remote " + profile.Name,
				ID:     providerID,
				Env:    []string{},
				NPM:    "@ai-sdk/openai-compatible",
				Models: models,
				Options: map[string]any{
					"baseURL": strings.TrimSpace(profile.BaseURL),
				},
			},
		},
		Model:        providerID + "/" + strings.TrimSpace(profile.Model),
		SmallModel:   prefixedModel(providerID, profile.SmallModel),
		Agent:        openCodeAgentModelOverrides(providerID, profile.SubagentModel),
		Instructions: strings.TrimSpace(profile.Instruction),
		Permission:   openCodePermissionMode(profile.PermissionMode),
	})
	if err != nil {
		return LaunchMaterial{}, err
	}
	authRaw, err := json.Marshal(authOverlay{
		providerID: {Type: "api", Key: profile.APIKey},
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

func addModelOverlay(models map[string]modelOverlay, model, reasoningEffort string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	if _, exists := models[model]; exists {
		return
	}
	models[model] = modelOverlay{
		ID:          model,
		Name:        model,
		Attachment:  false,
		Reasoning:   strings.TrimSpace(reasoningEffort) != "",
		Temperature: false,
		ToolCall:    true,
		ReleaseDate: "2025-01-01",
		Limit:       map[string]int{"context": 100000, "output": 10000},
		Cost:        map[string]float64{"input": 0, "output": 0},
		Variants:    openCodeReasoningVariants(reasoningEffort),
		Options:     map[string]any{},
	}
}

func prefixedModel(providerID, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	return providerID + "/" + model
}

func openCodeAgentModelOverrides(providerID, subagentModel string) map[string]agentOverlay {
	model := prefixedModel(providerID, subagentModel)
	if model == "" {
		return nil
	}
	return map[string]agentOverlay{
		"general": {Model: model},
		"explore": {Model: model},
	}
}

func openCodePermissionMode(value string) map[string]string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ask", "allow", "deny":
		return map[string]string{"*": strings.ToLower(strings.TrimSpace(value))}
	default:
		return nil
	}
}

func openCodeReasoningVariants(value string) map[string]map[string]any {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	return map[string]map[string]any{value: {}}
}
