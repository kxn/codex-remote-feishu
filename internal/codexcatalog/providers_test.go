package codexcatalog

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentifyEmbeddedCatalog(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		model   string
		want    string // expected Kind, "" means no match
	}{
		// 端点命中
		{name: "deepseek official endpoint", baseURL: "https://api.deepseek.com/", model: "gpt-5.5", want: "deepseek"},
		{name: "deepseek endpoint no scheme", baseURL: "api.deepseek.com/v1", model: "gpt-5.5", want: "deepseek"},
		{name: "mimo pay-as-you-go endpoint", baseURL: "https://api.xiaomimimo.com/v1", model: "custom", want: "mimo"},
		{name: "mimo token plan endpoint", baseURL: "https://token-plan-cn.xiaomimimo.com/v1", model: "custom", want: "mimo"},
		// 模型名前缀命中（sub2api 中转等任意端点）
		{name: "deepseek model via proxy", baseURL: "https://my-proxy.example.com/v1", model: "deepseek-v4-flash", want: "deepseek"},
		{name: "mimo model via proxy", baseURL: "https://my-proxy.example.com/v1", model: "mimo-v2.5-pro", want: "mimo"},
		{name: "mimo model uppercase", baseURL: "https://my-proxy.example.com/v1", model: "MIMO-v2.5", want: "mimo"},
		// 双命中（端点 + 前缀同 provider）不冲突
		{name: "mimo endpoint and model", baseURL: "https://token-plan-cn.xiaomimimo.com/v1", model: "mimo-v2.5", want: "mimo"},
		// 未命中
		{name: "unknown model unknown endpoint", baseURL: "https://proxy.example/v1", model: "provider-custom", want: ""},
		{name: "gpt model deepseek endpoint", baseURL: "https://api.deepseek.com/", model: "gpt-5.5", want: "deepseek"},
		{name: "empty", baseURL: "", model: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog, ok := IdentifyEmbeddedCatalog(tt.baseURL, tt.model)
			if tt.want == "" {
				if ok {
					t.Fatalf("IdentifyEmbeddedCatalog(%q, %q) = %s, want no match", tt.baseURL, tt.model, catalog.Kind)
				}
				return
			}
			if !ok || catalog.Kind != tt.want {
				t.Fatalf("IdentifyEmbeddedCatalog(%q, %q) = %s/%v, want %s", tt.baseURL, tt.model, catalog.Kind, ok, tt.want)
			}
		})
	}
}

func TestEmbeddedCatalogPath(t *testing.T) {
	dir := ManagedModelCatalogDir("/var/lib/codex-remote")
	if got := MimoCatalog.CatalogPath(dir); got != filepath.Join(dir, MimoModelCatalogFileName) {
		t.Fatalf("MimoCatalog.CatalogPath = %q", got)
	}
	if got := DeepSeekCatalog.CatalogPath(dir); got != filepath.Join(dir, DeepSeekModelCatalogFileName) {
		t.Fatalf("DeepSeekCatalog.CatalogPath = %q", got)
	}
	if got := MimoCatalog.CatalogPath(""); got != "" {
		t.Fatalf("empty dir should not produce catalog path, got %q", got)
	}
}

func TestMimoModelCatalogJSON(t *testing.T) {
	raw := MimoModelCatalogJSON()
	if len(raw) == 0 {
		t.Fatal("expected embedded MiMo model catalog")
	}
	raw[0] = ' '
	if again := MimoModelCatalogJSON(); len(again) == 0 || again[0] != '{' {
		t.Fatal("MimoModelCatalogJSON must return a defensive copy")
	}

	var catalog struct {
		Models []struct {
			Slug                  string  `json:"slug"`
			ApplyPatchToolType    *string `json:"apply_patch_tool_type"`
			ContextWindow         int     `json:"context_window"`
			MaxContextWindow      int     `json:"max_context_window"`
			DefaultReasoningLevel string  `json:"default_reasoning_level"`
			BaseInstructions      string  `json:"base_instructions"`
			TruncationPolicy      struct {
				Mode string `json:"mode"`
			} `json:"truncation_policy"`
			SupportsParallelToolCalls bool `json:"supports_parallel_tool_calls"`
			SupportedReasoningLevel   []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
			ModelMessages *struct {
				InstructionsTemplate string `json:"instructions_template"`
			} `json:"model_messages"`
		} `json:"models"`
	}
	if err := json.Unmarshal(MimoModelCatalogJSON(), &catalog); err != nil {
		t.Fatalf("MiMo model catalog must be valid JSON: %v", err)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("expected two MiMo models, got %#v", catalog.Models)
	}
	if catalog.Models[0].Slug != "mimo-v2.5-pro" || catalog.Models[1].Slug != "mimo-v2.5" {
		t.Fatalf("unexpected MiMo model order: %#v", catalog.Models)
	}
	for _, model := range catalog.Models {
		if model.ApplyPatchToolType != nil {
			t.Fatalf("MiMo model %s must not advertise apply_patch_tool_type, got %q", model.Slug, *model.ApplyPatchToolType)
		}
		if model.ContextWindow != 1048576 || model.MaxContextWindow != 1048576 {
			t.Fatalf("unexpected context window for %s: %#v", model.Slug, model)
		}
		if model.DefaultReasoningLevel != "high" {
			t.Fatalf("default reasoning for %s = %q, want high", model.Slug, model.DefaultReasoningLevel)
		}
		if model.BaseInstructions == "" || !strings.HasPrefix(model.BaseInstructions, "You are MiMo") {
			t.Fatalf("expected %s to keep official MiMo base_instructions", model.Slug)
		}
		if model.ModelMessages == nil || model.ModelMessages.InstructionsTemplate == "" {
			t.Fatalf("expected %s to carry model_messages instructions template", model.Slug)
		}
		if model.SupportsParallelToolCalls {
			t.Fatalf("expected %s to disable parallel tool calls per MiMo docs", model.Slug)
		}
		got := make([]string, 0, len(model.SupportedReasoningLevel))
		for _, effort := range model.SupportedReasoningLevel {
			got = append(got, effort.Effort)
		}
		if len(got) != 2 || got[0] != "none" || got[1] != "high" {
			t.Fatalf("supported reasoning for %s = %#v, want none/high", model.Slug, got)
		}
	}
	if catalog.Models[0].TruncationPolicy.Mode != "bytes" {
		t.Fatalf("expected bytes truncation_policy on %s, got %q", catalog.Models[0].Slug, catalog.Models[0].TruncationPolicy.Mode)
	}
}

func TestBuildEmbeddedModelCatalogUsesMimoFallback(t *testing.T) {
	// 未知 mimo 模型必须用 mimo 模板（mimo-v2.5）生成，而不是 deepseek 模板。
	raw := BuildEmbeddedModelCatalog(MimoCatalog, []string{"mimo-v2.5-future"})
	if len(raw) == 0 {
		t.Fatal("expected embedded MiMo catalog JSON")
	}
	var catalog struct {
		Models []struct {
			Slug       string `json:"slug"`
			BaseInstr  string `json:"base_instructions"`
			Truncation struct {
				Mode string `json:"mode"`
			} `json:"truncation_policy"`
			SupportedReasoning []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("BuildEmbeddedModelCatalog produced invalid JSON: %v", err)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].Slug != "mimo-v2.5-future" {
		t.Fatalf("unexpected models: %#v", catalog.Models)
	}
	entry := catalog.Models[0]
	if !strings.HasPrefix(entry.BaseInstr, "You are MiMo") {
		t.Fatalf("unknown MiMo model must inherit MiMo template, got base_instructions=%q", entry.BaseInstr)
	}
	if entry.Truncation.Mode != "bytes" {
		t.Fatalf("unknown MiMo model must inherit bytes truncation policy, got %q", entry.Truncation.Mode)
	}
	got := make([]string, 0, len(entry.SupportedReasoning))
	for _, effort := range entry.SupportedReasoning {
		got = append(got, effort.Effort)
	}
	if len(got) != 2 || got[0] != "none" || got[1] != "high" {
		t.Fatalf("unknown MiMo model reasoning levels = %#v, want none/high", got)
	}
}

func TestBuildEmbeddedModelCatalogReusesMimoEntry(t *testing.T) {
	raw := BuildEmbeddedModelCatalog(MimoCatalog, []string{"mimo-v2.5-pro"})
	var catalog struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("BuildEmbeddedModelCatalog produced invalid JSON: %v", err)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].Slug != "mimo-v2.5-pro" || catalog.Models[0].DisplayName != "mimo-v2.5-pro" {
		t.Fatalf("expected embedded MiMo entry reuse, got %#v", catalog.Models)
	}
}
