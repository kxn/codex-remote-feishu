package codexcatalog

import (
	"embed"
	"encoding/json"
	"path/filepath"
	"strings"
)

const (
	DeepSeekModelCatalogFileName = "deepseek-models-v1.json"
	MimoModelCatalogFileName     = "mimo-models-v1.json"
	managedModelCatalogDirName   = "codex-model-catalogs"
)

//go:embed deepseek_models.json mimo_models.json
var embeddedCatalogsFS embed.FS

// IsDeepSeekProfile 报告 baseURL/模型名是否命中 DeepSeek 内置模型目录。
func IsDeepSeekProfile(baseURL, model string) bool {
	catalog, ok := IdentifyEmbeddedCatalog(baseURL, model)
	return ok && catalog.Kind == "deepseek"
}

func IsDeepSeekEndpoint(baseURL string) bool {
	return DeepSeekCatalog.MatchesEndpoint(baseURL)
}

func ManagedModelCatalogDir(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, managedModelCatalogDirName)
}

func DeepSeekModelCatalogPath(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, DeepSeekModelCatalogFileName)
}

func DeepSeekModelCatalogJSON() []byte {
	return DeepSeekCatalog.CatalogJSON()
}

func MimoModelCatalogJSON() []byte {
	return MimoCatalog.CatalogJSON()
}

// BuildEmbeddedModelCatalog 以指定内置目录为模板，生成只包含请求模型子集的
// 模型目录：已知 slug 复用内置条目，未知模型以该目录的 FallbackSlug 为模板
// 生成元数据（保证 codex 的 spawn_agent 能找到该模型）。
func BuildEmbeddedModelCatalog(catalog EmbeddedCatalog, models []string) []byte {
	seen := map[string]bool{}
	requested := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		requested = append(requested, model)
	}
	if len(requested) == 0 {
		return nil
	}

	type catalogFile struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	var embedded catalogFile
	if err := json.Unmarshal(catalog.CatalogJSON(), &embedded); err != nil {
		return nil
	}
	embeddedBySlug := make(map[string]map[string]json.RawMessage, len(embedded.Models))
	for _, entry := range embedded.Models {
		var slug string
		_ = json.Unmarshal(entry["slug"], &slug)
		embeddedBySlug[strings.TrimSpace(slug)] = entry
	}

	base, ok := embeddedBySlug[catalog.FallbackSlug]
	if !ok {
		return nil
	}
	modelsOut := make([]map[string]json.RawMessage, 0, len(requested))
	for _, model := range requested {
		if entry, ok := embeddedBySlug[model]; ok {
			modelsOut = append(modelsOut, cloneRawEntry(entry))
			continue
		}
		entry := cloneRawEntry(base)
		entry["slug"], _ = json.Marshal(model)
		entry["display_name"], _ = json.Marshal(model)
		entry["description"], _ = json.Marshal("Model provided by Codex Remote profile.")
		entry["priority"], _ = json.Marshal(100)
		modelsOut = append(modelsOut, entry)
	}
	raw, err := json.Marshal(catalogFile{Models: modelsOut})
	if err != nil {
		return nil
	}
	return raw
}

func cloneRawEntry(entry map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(entry))
	for key, value := range entry {
		cloned[key] = append([]byte(nil), value...)
	}
	return cloned
}
