package control

import (
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/buildinfo"
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
	policy := buildinfo.CurrentCapabilityPolicy()
	formHints := []string{"track", "latest"}
	examples := []string{"/upgrade", "/upgrade latest"}
	options := []FeishuCommandOption{
		commandOption("/upgrade", "upgrade", "track", "查看 Track", "查看当前 track。"),
		commandOption("/upgrade", "upgrade", "latest", "检查/继续升级", "检查或继续升级到当前 track 的最新 release。"),
	}
	if trackExample := preferredUpgradeTrackExample(policy.AllowedReleaseTracks); trackExample != "" {
		examples = append(examples, "/upgrade track "+trackExample)
	}
	for _, track := range policy.AllowedReleaseTracks {
		track = strings.TrimSpace(track)
		if track == "" {
			continue
		}
		options = append(options, commandOption("/upgrade track", "upgrade_track", track, track, "切换到 "+track+" track。"))
	}
	description := "查看升级状态、查看或切换当前 release track；`/upgrade latest` 检查或继续 release 升级。"
	if policy.AllowDevUpgrade {
		formHints = append(formHints, "dev")
		examples = append(examples, "/upgrade dev")
		options = append(options, commandOption("/upgrade", "upgrade", "dev", "开发构建", "检查或继续升级到最新的 dev 构建。"))
		description += " `/upgrade dev` 检查或继续 dev 构建升级。"
	}
	if policy.AllowLocalUpgrade {
		formHints = append(formHints, "local")
		examples = append(examples, "/upgrade local")
		options = append(options, commandOption("/upgrade", "upgrade", "local", "本地升级", "使用固定本地 artifact 发起升级。"))
		description += " `/upgrade local` 使用固定本地 artifact 发起升级。"
	}
	def.ArgumentFormHint = "track"
	def.ArgumentFormNote = "例如 " + strings.Join(formHints, "、") + "。"
	def.Description = description
	def.Examples = examples
	def.Options = options
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

func preferredUpgradeTrackExample(allowed []string) string {
	for _, candidate := range []string{"beta", "production", "alpha"} {
		for _, track := range allowed {
			if strings.EqualFold(strings.TrimSpace(track), candidate) {
				return candidate
			}
		}
	}
	return ""
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
