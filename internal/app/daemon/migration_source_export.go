package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PersonalMigrationSource is a read-only snapshot, never a drain or cutover.
type PersonalMigrationSource struct {
	Source   workstore.PersonalExport  `json:"source"`
	Settings PersonalMigrationSettings `json:"settings"`
	SHA256   string                    `json:"sha256"`
}
type PersonalMigrationSettings struct {
	RoleID                    string             `json:"role_id"`
	SettingsRevision          int64              `json:"settings_revision"`
	SourceFingerprint         string             `json:"source_fingerprint"`
	GatewayRef                string             `json:"gateway_ref"`
	ProductMode               string             `json:"product_mode,omitempty"`
	Backend                   agentproto.Backend `json:"backend"`
	ProfileID                 string             `json:"profile_id"`
	ProfileRevision           uint64             `json:"profile_revision,omitempty"`
	ConnectionGeneration      uint64             `json:"connection_generation,omitempty"`
	CredentialGeneration      uint64             `json:"credential_generation,omitempty"`
	ProfileKind               string             `json:"profile_kind,omitempty"`
	ProfileName               string             `json:"profile_name,omitempty"`
	ProfilePreferenceRevision uint64             `json:"profile_preference_revision,omitempty"`
	ProfilePreferenceMode     string             `json:"profile_preference_mode,omitempty"`
	BotModel                  string             `json:"bot_model,omitempty"`
	BotReasoningEffort        string             `json:"bot_reasoning_effort,omitempty"`
	Model                     string             `json:"model,omitempty"`
	SmallModel                string             `json:"small_model,omitempty"`
	ReviewModel               string             `json:"review_model,omitempty"`
	SubagentModel             string             `json:"subagent_model,omitempty"`
	ReasoningEffort           string             `json:"reasoning_effort,omitempty"`
	Instruction               string             `json:"instruction,omitempty"`
	AccessMode                string             `json:"access_mode,omitempty"`
	PlanMode                  string             `json:"plan_mode,omitempty"`
	PlanModeOverrideSet       bool               `json:"plan_mode_override_set"`
	ProjectConfigMode         string             `json:"project_config_mode,omitempty"`
	DataIsolationMode         string             `json:"data_isolation_mode,omitempty"`
	PermissionMode            string             `json:"permission_mode,omitempty"`
	VisionSupported           bool               `json:"vision_supported"`
	CurrentAccountIdentity    string             `json:"current_account_identity"`
	CurrentNativeNamespace    string             `json:"current_native_namespace"`
}

func (s PersonalMigrationSource) Verify() error {
	if err := s.Source.Verify(); err != nil {
		return err
	}
	expected := s.SHA256
	s.SHA256 = ""
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	if expected == "" || expected != hex.EncodeToString(hash[:]) {
		return fmt.Errorf("personal migration source integrity mismatch")
	}
	return nil
}

func (a *App) capturePersonalMigrationSource(ctx context.Context, store *workstore.Store, scopeID string, limits workstore.PersonalExportLimits) (PersonalMigrationSource, error) {
	var out PersonalMigrationSource
	var scope work.WorkScope
	var binding work.RoleBotBinding
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		scope, e = tx.GetScope(scopeID)
		if e != nil {
			return e
		}
		binding, e = tx.GetRoleBotBinding(scope.RoleID)
		return e
	}); err != nil {
		return out, err
	}
	if scope.Kind != work.ScopePersonal {
		return out, workstore.ErrScopeDenied
	}
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return out, err
	}
	configured := false
	for _, gateway := range a.runtimeGatewayApps(loaded.Config) {
		if gateway.Enabled && gateway.GatewayID == binding.GatewayRef && gateway.AppID == binding.AppID {
			configured = true
			break
		}
	}
	if !configured {
		return out, workstore.ErrConflict
	}
	a.mu.Lock()
	record := a.workBotSettingsLocked(binding.GatewayRef)
	resolved := a.resolveWorkBotSettings(record, loaded.Config)
	var preferenceRevision uint64
	var preferenceMode string
	if preferences, preferenceErr := a.profileContextPreferenceStore(); preferenceErr == nil {
		var found bool
		var preference state.ProfileContextPreference
		switch record.Backend {
		case agentproto.BackendClaude:
			preference, found = preferences.ClaudeCurrent(teamSettingsProfile(record))
		case agentproto.BackendCodex:
			preference, found = preferences.CodexCurrent(teamSettingsProfile(record))
		}
		if found {
			preferenceRevision, preferenceMode = preference.Revision, preference.Mode
		}
	}
	a.mu.Unlock()
	if resolved.err != nil {
		return out, resolved.err
	}
	settings := PersonalMigrationSettings{RoleID: scope.RoleID, GatewayRef: binding.GatewayRef, SourceFingerprint: resolved.fingerprint, Backend: record.Backend, ProfileID: teamSettingsProfile(record), BotModel: record.PromptOverride.Model, BotReasoningEffort: record.PromptOverride.ReasoningEffort, AccessMode: record.PromptOverride.AccessMode, PlanMode: string(record.PlanMode), CurrentAccountIdentity: "unavailable", CurrentNativeNamespace: "unavailable"}
	settings.ProfilePreferenceRevision, settings.ProfilePreferenceMode = preferenceRevision, preferenceMode
	settings.ProductMode, settings.PlanModeOverrideSet = string(record.ProductMode), record.PlanModeOverrideSet
	switch record.Backend {
	case agentproto.BackendClaude:
		p := resolved.claude
		settings.ProfileName = p.Name
		settings.ProfileKind = p.AuthMode
		settings.Model = p.Model
		settings.SmallModel = p.SmallModel
		settings.SubagentModel = p.SubagentModel
		settings.Instruction = p.Instruction
		settings.ReasoningEffort = p.ReasoningEffort
		settings.VisionSupported = p.VisionSupported
	case agentproto.BackendOpenCode:
		p := resolved.openCode
		settings.ProfileName = p.Name
		settings.ProfileKind = p.ProviderType
		settings.ProfileRevision = p.Revision
		settings.ConnectionGeneration = p.ConnectionGeneration
		settings.CredentialGeneration = p.CredentialGeneration
		settings.Model = p.Model
		settings.SmallModel = p.SmallModel
		settings.ReviewModel = p.ReviewModel
		settings.SubagentModel = p.SubagentModel
		settings.Instruction = p.Instruction
		settings.ReasoningEffort = p.ReasoningEffort
		settings.VisionSupported = p.VisionSupported
		settings.ProjectConfigMode, settings.DataIsolationMode, settings.PermissionMode = p.ProjectConfigMode, p.DataIsolationMode, p.PermissionMode
	default:
		idx := config.IndexOfCodexAPIProfile(loaded.Config.Codex.Profiles, settings.ProfileID)
		if idx >= 0 {
			p, ok := config.CurrentCodexAPIProfile(loaded.Config.Codex.Profiles[idx])
			if !ok {
				return out, workstore.ErrConflict
			}
			settings.ProfileName = p.Name
			settings.ProfileKind = string(p.Kind)
			settings.ProfileRevision = p.Revision
			settings.ConnectionGeneration = p.ConnectionGeneration
			settings.CredentialGeneration = p.CredentialGeneration
			settings.Model = p.Model
			settings.ReviewModel = p.ReviewModel
			settings.SubagentModel = p.SubagentModel
			settings.Instruction = p.Instruction
			settings.ReasoningEffort = p.ReasoningEffort
			settings.VisionSupported = p.VisionSupported
		}
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetScope(scopeID)
		if e != nil {
			return e
		}
		if current != scope {
			return workstore.ErrConflict
		}
		currentBinding, e := tx.GetRoleBotBinding(scope.RoleID)
		if e != nil {
			return e
		}
		if currentBinding != binding {
			return workstore.ErrConflict
		}
		role, e := tx.GetRole(scope.RoleID)
		if e != nil {
			return e
		}
		if e = tx.CheckRoleSettingsReference(role.ID, resolved.fingerprint, role.SettingsRevision); e != nil {
			return e
		}
		settings.SettingsRevision = role.SettingsRevision
		out.Source, e = tx.CapturePersonalExport(scopeID, limits)
		return e
	}); err != nil {
		return PersonalMigrationSource{}, err
	}
	a.mu.Lock()
	current := a.workBotSettingsLocked(binding.GatewayRef)
	fingerprint := a.resolveWorkBotSettings(current, loaded.Config).fingerprint
	a.mu.Unlock()
	if fingerprint != resolved.fingerprint || !reflect.DeepEqual(record, current) {
		return PersonalMigrationSource{}, workstore.ErrConflict
	}
	out.Settings = settings
	raw, err := json.Marshal(out)
	if err != nil {
		return PersonalMigrationSource{}, err
	}
	if int64(len(raw))+64 > limits.MaxBytes {
		return PersonalMigrationSource{}, fmt.Errorf("personal migration source exceeds configured byte limit")
	}
	hash := sha256.Sum256(raw)
	out.SHA256 = hex.EncodeToString(hash[:])
	return out, nil
}
