package control

import (
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/buildinfo"
	"github.com/kxn/codex-remote-feishu/internal/core/upgradecontract"
)

func runtimeFeishuCommandDefinition(spec feishuCommandSpec) FeishuCommandDefinition {
	def := cloneFeishuCommandDefinition(spec.definition)
	switch def.ID {
	case FeishuCommandAdmin:
		return runtimeAdminCommandDefinition(def)
	case FeishuCommandUpgrade:
		return runtimeUpgradeCommandDefinition(def)
	case FeishuCommandDebug:
		return runtimeDebugCommandDefinition(def)
	default:
		return def
	}
}

func runtimeAdminCommandDefinition(def FeishuCommandDefinition) FeishuCommandDefinition {
	def.Description = "打开系统管理入口；可从这里访问管理页、自动启动和维护命令。"
	def.Examples = []string{"/admin web", "/admin localweb"}
	if feishuAdminAutostartSupportedPlatform(runtime.GOOS) {
		def.Description += " `/admin autostart on|off` 用于配置自动启动。"
		def.Examples = append(def.Examples, "/admin autostart on", "/admin autostart off")
	}
	return def
}

func runtimeUpgradeCommandDefinition(def FeishuCommandDefinition) FeishuCommandDefinition {
	metadata := upgradecontract.BuildDefinition(upgradeCapabilityPolicyFromBuildInfo())
	def.ArgumentFormHint = metadata.ArgumentFormHint
	def.ArgumentFormNote = metadata.ArgumentFormNote
	def.Description = metadata.Description
	def.Examples = append([]string(nil), metadata.Examples...)
	def.Options = make([]FeishuCommandOption, 0, len(metadata.Options))
	for _, option := range metadata.Options {
		def.Options = append(def.Options, FeishuCommandOption{
			Value:       option.Value,
			Label:       option.Label,
			Description: option.Description,
			CommandText: option.CommandText,
			MenuKey:     option.MenuKey,
		})
	}
	return def
}

func runtimeDebugCommandDefinition(def FeishuCommandDefinition) FeishuCommandDefinition {
	def.ArgumentFormHint = ""
	def.ArgumentFormNote = ""
	def.Description = "查看调试入口；管理页相关功能已迁移到 `/admin`。"
	def.Examples = []string{"/debug"}
	def.Options = nil
	return def
}

func upgradeCapabilityPolicyFromBuildInfo() upgradecontract.CapabilityPolicy {
	policy := buildinfo.CurrentCapabilityPolicy()
	return upgradecontract.CapabilityPolicy{
		AllowedReleaseTracks: upgradecontract.NormalizeReleaseTracks(policy.AllowedReleaseTracks),
		AllowDevUpgrade:      policy.AllowDevUpgrade,
		AllowLocalUpgrade:    policy.AllowLocalUpgrade,
	}
}

func FeishuCommandForm(commandID string) (*CommandCatalogForm, bool) {
	def, ok := FeishuCommandDefinitionByID(commandID)
	if !ok {
		return nil, false
	}
	switch def.ArgumentKind {
	case FeishuCommandArgumentChoice, FeishuCommandArgumentText:
	default:
		return nil, false
	}
	submit := strings.TrimSpace(def.ArgumentSubmit)
	if submit == "" {
		submit = "执行"
	}
	label := strings.TrimSpace(def.ArgumentFormNote)
	if label == "" {
		label = "输入这条命令后面的参数。"
	}
	return &CommandCatalogForm{
		CommandID:   def.ID,
		CommandText: def.CanonicalSlash,
		SubmitLabel: submit,
		Field: CommandCatalogFormField{
			Name:        "command_args",
			Kind:        CommandCatalogFormFieldText,
			Label:       label,
			Placeholder: strings.TrimSpace(def.ArgumentFormHint),
		},
	}, true
}

func FeishuCommandFormWithDefault(commandID, defaultValue string) *CommandCatalogForm {
	form, ok := FeishuCommandForm(commandID)
	if !ok || form == nil {
		return nil
	}
	cloned := *form
	cloned.Field = form.Field
	cloned.Field.DefaultValue = strings.TrimSpace(defaultValue)
	if len(form.Field.Options) > 0 {
		cloned.Field.Options = append([]CommandCatalogFormFieldOption(nil), form.Field.Options...)
	}
	return &cloned
}
