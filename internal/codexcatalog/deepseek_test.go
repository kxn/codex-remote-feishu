package codexcatalog

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsDeepSeekEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{name: "official endpoint", baseURL: "https://api.deepseek.com/", want: true},
		{name: "official endpoint with path", baseURL: "https://api.deepseek.com/v1", want: true},
		{name: "official endpoint without scheme", baseURL: "api.deepseek.com/v1", want: true},
		{name: "official endpoint with port", baseURL: "https://api.deepseek.com:443/v1", want: true},
		{name: "lookalike host", baseURL: "https://api.deepseek.com.evil/v1", want: false},
		{name: "other endpoint", baseURL: "https://proxy.example/v1", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDeepSeekEndpoint(tt.baseURL); got != tt.want {
				t.Fatalf("IsDeepSeekEndpoint(%q) = %v, want %v", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestIsDeepSeekProfile(t *testing.T) {
	if !IsDeepSeekProfile("https://proxy.example/v1", "deepseek-v4-flash") {
		t.Fatal("expected deepseek model prefix to identify DeepSeek profile")
	}
	if !IsDeepSeekProfile("https://api.deepseek.com/", "gpt-5.5") {
		t.Fatal("expected DeepSeek endpoint to identify DeepSeek profile")
	}
	if IsDeepSeekProfile("https://proxy.example/v1", "provider-custom") {
		t.Fatal("expected non-DeepSeek profile to stay generic")
	}
}

func TestManagedDeepSeekModelCatalogPath(t *testing.T) {
	dir := ManagedModelCatalogDir("/var/lib/codex-remote")
	if want := filepath.Join("/var/lib/codex-remote", "codex-model-catalogs"); dir != want {
		t.Fatalf("ManagedModelCatalogDir = %q, want %q", dir, want)
	}
	if got := DeepSeekModelCatalogPath(dir); got != filepath.Join(dir, DeepSeekModelCatalogFileName) {
		t.Fatalf("DeepSeekModelCatalogPath = %q", got)
	}
	if got := ManagedModelCatalogPath(dir); got != filepath.Join(dir, ManagedModelCatalogFileName) {
		t.Fatalf("ManagedModelCatalogPath = %q", got)
	}
	if got := ManagedModelCatalogDir(""); got != "" {
		t.Fatalf("empty state dir should not produce managed dir, got %q", got)
	}
}

func TestBuildManagedModelCatalogIncludesRequestedModels(t *testing.T) {
	raw := BuildManagedModelCatalog([]string{"gpt-5.6", "gpt-5.6-nano"})
	if len(raw) == 0 {
		t.Fatal("expected managed model catalog JSON")
	}
	var catalog struct {
		Models []struct {
			Slug                    string `json:"slug"`
			ContextWindow           int    `json:"context_window"`
			DefaultReasoningLevel   string `json:"default_reasoning_level"`
			MultiAgentVersion       string `json:"multi_agent_version"`
			SupportedReasoningLevel []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
			ModelMessages *struct {
				InstructionsTemplate string `json:"instructions_template"`
			} `json:"model_messages"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("BuildManagedModelCatalog produced invalid JSON: %v", err)
	}
	slugs := map[string]bool{}
	for _, model := range catalog.Models {
		slugs[model.Slug] = true
		if model.ContextWindow <= 0 || model.DefaultReasoningLevel == "" || len(model.SupportedReasoningLevel) == 0 || model.ModelMessages == nil {
			t.Fatalf("generic entry incomplete: %#v", model)
		}
	}
	if !slugs["gpt-5.6"] || !slugs["gpt-5.6-nano"] {
		t.Fatalf("managed catalog missing requested models: %#v", slugs)
	}
}

func TestBuildManagedModelCatalogReusesDeepSeekEntry(t *testing.T) {
	raw := BuildManagedModelCatalog([]string{"deepseek-v4-flash"})
	var catalog struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("BuildManagedModelCatalog produced invalid JSON: %v", err)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].Slug != "deepseek-v4-flash" || catalog.Models[0].DisplayName != "DeepSeek-V4-Flash" {
		t.Fatalf("expected embedded DeepSeek entry reuse, got %#v", catalog.Models)
	}
}

func TestBuildManagedModelCatalogWithInstructionAppendsToAllModels(t *testing.T) {
	raw := BuildManagedModelCatalogWithInstruction([]string{"gpt-5.6", "gpt-5.6-nano"}, "你是一个严谨的工程师。")
	if len(raw) == 0 {
		t.Fatal("expected managed model catalog JSON with instruction")
	}
	var catalog struct {
		Models []struct {
			Slug          string `json:"slug"`
			ModelMessages *struct {
				InstructionsTemplate string `json:"instructions_template"`
			} `json:"model_messages"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("BuildManagedModelCatalogWithInstruction produced invalid JSON: %v", err)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("expected two managed models, got %#v", catalog.Models)
	}
	for _, model := range catalog.Models {
		if model.ModelMessages == nil {
			t.Fatalf("model %s missing model_messages", model.Slug)
		}
		template := model.ModelMessages.InstructionsTemplate
		if !strings.HasPrefix(template, "You are Codex") {
			t.Fatalf("model %s lost base instructions prefix", model.Slug)
		}
		if !strings.HasSuffix(template, "\n\n你是一个严谨的工程师。") {
			t.Fatalf("model %s instruction suffix = %q, want appended role prompt", model.Slug, template)
		}
	}
}

func TestBuildManagedModelCatalogWithInstructionEmptyKeepsTemplateUnchanged(t *testing.T) {
	models := []string{"gpt-5.6", "gpt-5.6-nano"}
	plain := BuildManagedModelCatalog(models)
	withEmpty := BuildManagedModelCatalogWithInstruction(models, "   ")
	if string(plain) != string(withEmpty) {
		t.Fatal("empty instruction must not modify the managed catalog")
	}
}

func TestAppendCatalogInstructionPreservesFullDeepSeekCatalog(t *testing.T) {
	raw := AppendCatalogInstruction(DeepSeekModelCatalogJSON(), "你是一个乐于助人的助手。")
	if len(raw) == 0 {
		t.Fatal("expected DeepSeek catalog JSON with instruction")
	}
	var catalog struct {
		Models []struct {
			Slug          string `json:"slug"`
			ContextWindow int    `json:"context_window"`
			ModelMessages *struct {
				InstructionsTemplate string `json:"instructions_template"`
			} `json:"model_messages"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("AppendCatalogInstruction produced invalid JSON: %v", err)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("expected two DeepSeek models, got %#v", catalog.Models)
	}
	for _, model := range catalog.Models {
		if model.ContextWindow != 1048576 {
			t.Fatalf("model %s context window changed: %#v", model.Slug, model)
		}
		if model.ModelMessages == nil {
			t.Fatalf("model %s missing model_messages", model.Slug)
		}
		if !strings.HasSuffix(model.ModelMessages.InstructionsTemplate, "\n\n你是一个乐于助人的助手。") {
			t.Fatalf("model %s instruction suffix = %q, want appended role prompt", model.Slug, model.ModelMessages.InstructionsTemplate)
		}
	}
}

func TestDeepSeekModelCatalogJSON(t *testing.T) {
	raw := DeepSeekModelCatalogJSON()
	if len(raw) == 0 {
		t.Fatal("expected embedded DeepSeek model catalog")
	}
	raw[0] = ' '
	if again := DeepSeekModelCatalogJSON(); len(again) == 0 || again[0] != '{' {
		t.Fatal("DeepSeekModelCatalogJSON must return a defensive copy")
	}

	var catalog struct {
		Models []struct {
			Slug                    string `json:"slug"`
			ContextWindow           int    `json:"context_window"`
			MaxContextWindow        int    `json:"max_context_window"`
			DefaultReasoningLevel   string `json:"default_reasoning_level"`
			BaseInstructions        string `json:"base_instructions"`
			SupportedReasoningLevel []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
			ModelMessages *struct {
				InstructionsTemplate string `json:"instructions_template"`
			} `json:"model_messages"`
		} `json:"models"`
	}
	if err := json.Unmarshal(DeepSeekModelCatalogJSON(), &catalog); err != nil {
		t.Fatalf("DeepSeek model catalog must be valid JSON: %v", err)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("expected two DeepSeek models, got %#v", catalog.Models)
	}
	for _, model := range catalog.Models {
		if model.ContextWindow != 1048576 || model.MaxContextWindow != 1048576 {
			t.Fatalf("unexpected context window for %s: %#v", model.Slug, model)
		}
		if model.DefaultReasoningLevel != "high" {
			t.Fatalf("default reasoning for %s = %q, want high", model.Slug, model.DefaultReasoningLevel)
		}
		if model.ModelMessages == nil || model.ModelMessages.InstructionsTemplate == "" {
			t.Fatalf("expected %s to keep official model_messages instructions template", model.Slug)
		}
		if model.BaseInstructions == "" {
			t.Fatalf("expected %s to keep official base_instructions", model.Slug)
		}
		got := make([]string, 0, len(model.SupportedReasoningLevel))
		for _, effort := range model.SupportedReasoningLevel {
			got = append(got, effort.Effort)
		}
		if len(got) != 3 || got[0] != "low" || got[1] != "high" || got[2] != "max" {
			t.Fatalf("supported reasoning for %s = %#v, want low/high/max", model.Slug, got)
		}
	}
	if catalog.Models[0].Slug != "deepseek-v4-flash" || catalog.Models[1].Slug != "deepseek-v4-pro" {
		t.Fatalf("unexpected DeepSeek model order: %#v", catalog.Models)
	}
}
