package config

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// InstructionMaxChars 是 Codex / Claude profile instruction 的统一上限（Unicode 字符数），
// 与 Web 表单的 maxLength / 计数保持一致。
const InstructionMaxChars = 16000

// ValidateInstruction 校验可选的角色提示词：不允许 NUL，且不能超过 InstructionMaxChars。
func ValidateInstruction(value string) error {
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("instruction contains null byte")
	}
	if utf8.RuneCountInString(value) > InstructionMaxChars {
		return fmt.Errorf("instruction exceeds %d characters", InstructionMaxChars)
	}
	return nil
}
