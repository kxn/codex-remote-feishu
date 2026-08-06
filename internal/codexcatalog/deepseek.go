package codexcatalog

import (
	"embed"
	"encoding/json"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

const (
	DeepSeekModelCatalogFileName = "deepseek-models-v1.json"
	ManagedModelCatalogFileName  = "managed-models-v1.json"
	managedModelCatalogDirName   = "codex-model-catalogs"
)

//go:embed deepseek_models.json
var embeddedDeepSeekModelCatalog embed.FS

func IsDeepSeekProfile(baseURL, model string) bool {
	return IsDeepSeekEndpoint(baseURL) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "deepseek-")
}

func IsDeepSeekEndpoint(baseURL string) bool {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return false
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		parsed, err = url.Parse("https://" + strings.TrimLeft(baseURL, "/"))
	}
	if err != nil || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		if splitHost, _, err := net.SplitHostPort(parsed.Host); err == nil {
			host = strings.ToLower(strings.TrimSpace(splitHost))
		}
	}
	return host == "api.deepseek.com"
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

func ManagedModelCatalogPath(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ManagedModelCatalogFileName)
}

func DeepSeekModelCatalogJSON() []byte {
	raw, err := embeddedDeepSeekModelCatalog.ReadFile("deepseek_models.json")
	if err != nil {
		return nil
	}
	return append([]byte(nil), raw...)
}

// BuildManagedModelCatalog 生成包含指定模型的模型目录 JSON。
// DeepSeek 已知模型直接复用内嵌条目；其他模型以 DeepSeek 条目为模板生成
// 保守的 fallback 元数据，保证 spawn_agent 能在目录中找到该模型。
func BuildManagedModelCatalog(models []string) []byte {
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
	if err := json.Unmarshal(DeepSeekModelCatalogJSON(), &embedded); err != nil {
		return nil
	}
	embeddedBySlug := make(map[string]map[string]json.RawMessage, len(embedded.Models))
	for _, entry := range embedded.Models {
		var slug string
		_ = json.Unmarshal(entry["slug"], &slug)
		embeddedBySlug[strings.TrimSpace(slug)] = entry
	}

	modelsOut := make([]map[string]json.RawMessage, 0, len(requested))
	for _, model := range requested {
		if entry, ok := embeddedBySlug[model]; ok {
			modelsOut = append(modelsOut, cloneRawEntry(entry))
			continue
		}
		base, ok := embeddedBySlug["deepseek-v4-flash"]
		if !ok {
			return nil
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
