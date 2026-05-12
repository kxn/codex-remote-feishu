package orchestrator

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func containsPromptSectionLine(section control.FeishuCardTextSection, want string) bool {
	for _, line := range section.Lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

func containsStatePromptSectionLine(section state.RequestPromptTextSectionRecord, want string) bool {
	for _, line := range section.Lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

func TestRequestPermissionLinesParsesClaudeRuleSuggestions(t *testing.T) {
	got := requestPermissionLines([]map[string]any{
		{
			"type":        "addRules",
			"behavior":    "allow",
			"destination": "localSettings",
			"rules": []any{
				map[string]any{"toolName": "Bash", "ruleContent": "ls:*"},
			},
		},
		{
			"type":        "addRules",
			"behavior":    "allow",
			"destination": "localSettings",
			"rules": []any{
				map[string]any{"toolName": "Bash", "ruleContent": "ls:*"},
			},
		},
	})
	want := []string{"- Bash: ls:*"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("permission lines = %#v, want %#v", got, want)
	}
}

func TestRequestPermissionLinesKeepsFlatPermissions(t *testing.T) {
	got := requestPermissionLines([]map[string]any{
		{"name": "docs.read", "title": "Read docs"},
		{"permission": "workspace.write"},
	})
	want := []string{"- Read docs (`docs.read`)", "- workspace.write"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("permission lines = %#v, want %#v", got, want)
	}
}

func TestRequestPermissionLinesSkipsUnparseableRecords(t *testing.T) {
	got := requestPermissionLines([]map[string]any{
		{"type": "addRules", "rules": []any{}},
		{"toolName": "Bash"},
		{"title": map[string]any{"unexpected": "shape"}},
	})
	if len(got) != 0 {
		t.Fatalf("expected unparseable permission records to be hidden, got %#v", got)
	}
}

func TestBuildRequestPromptBodySectionsUsesFallbackWhenBodyEmpty(t *testing.T) {
	got := buildRequestPromptBodySections("", "第一行\n第二行")
	want := []state.RequestPromptTextSectionRecord{{
		Lines: []string{"第一行", "第二行"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %#v, want %#v", got, want)
	}
}

func TestBuildRequestPromptBodySectionsPrefersBodyOverFallback(t *testing.T) {
	got := buildRequestPromptBodySections("正文", "fallback")
	want := []state.RequestPromptTextSectionRecord{{
		Lines: []string{"正文"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %#v, want %#v", got, want)
	}
}
