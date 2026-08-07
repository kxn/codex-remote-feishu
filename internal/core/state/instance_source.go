package state

import "strings"

// InstanceSource 表示实例的来源（InstanceRecord.Source 的合法值集合）。
// 单一事实来源：所有赋值与比较都必须引用本常量与 IsInstanceSource，
// 禁止在调用点内联 "headless" / "vscode" 字面量或混用 EqualFold / ==。
type InstanceSource string

const (
	// InstanceSourceHeadless 表示由本仓库 daemon 直接托管的 headless 实例。
	InstanceSourceHeadless InstanceSource = "headless"
	// InstanceSourceVSCode 表示由 VS Code 扩展连接的实例（历史默认来源）。
	InstanceSourceVSCode InstanceSource = "vscode"
)

// NormalizeInstanceSource 将任意来源字符串归一为已知来源；未知来源原样
// 保留（首尾空白去除），避免未知值被误判为默认来源。
func NormalizeInstanceSource(source string) InstanceSource {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "headless":
		return InstanceSourceHeadless
	case "vscode":
		return InstanceSourceVSCode
	default:
		return InstanceSource(strings.TrimSpace(source))
	}
}

// IsInstanceSource 大小写与首尾空白不敏感地比较来源是否为目标值。
// 这是 InstanceRecord.Source 比较的唯一入口，统一此前 EqualFold / == 混用。
func IsInstanceSource(source string, want InstanceSource) bool {
	return NormalizeInstanceSource(source) == want
}

// IsManagedHeadlessInstance 是 "托管 headless 实例" 判定的唯一实现：
// 来源为 headless 且实例被标记为托管。所有包（daemon / orchestrator /
// wrapper 等）的 managed-headless 判断都必须委托本函数，禁止内联复制。
func IsManagedHeadlessInstance(inst *InstanceRecord) bool {
	return inst != nil && IsInstanceSource(inst.Source, InstanceSourceHeadless) && inst.Managed
}

// IsVSCodeOrDefaultSource 报告来源是否视为 VS Code：空来源按 VS Code
// 处理（保持历史兜底语义：未声明来源的实例视作 VS Code 连接）。
func IsVSCodeOrDefaultSource(source string) bool {
	s := strings.TrimSpace(source)
	if s == "" {
		return true
	}
	return IsInstanceSource(s, InstanceSourceVSCode)
}
