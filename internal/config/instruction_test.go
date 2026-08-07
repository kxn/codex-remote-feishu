package config

import (
	"strings"
	"testing"
)

func TestValidateInstructionAcceptsEmptyAndWithinLimit(t *testing.T) {
	for _, value := range []string{"", "   ", "你是一个乐于助人的助手。", strings.Repeat("a", InstructionMaxChars)} {
		if err := ValidateInstruction(value); err != nil {
			t.Fatalf("ValidateInstruction(%d chars) = %v, want nil", len(value), err)
		}
	}
}

func TestValidateInstructionRejectsOversizedValue(t *testing.T) {
	if err := ValidateInstruction(strings.Repeat("a", InstructionMaxChars+1)); err == nil {
		t.Fatal("ValidateInstruction() accepted an instruction over the limit")
	}
}

func TestValidateInstructionRejectsNullByte(t *testing.T) {
	if err := ValidateInstruction("prefix\x00suffix"); err == nil {
		t.Fatal("ValidateInstruction() accepted a null byte")
	}
}
