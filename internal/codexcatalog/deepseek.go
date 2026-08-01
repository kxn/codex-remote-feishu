package codexcatalog

import (
	"embed"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

const (
	DeepSeekModelCatalogFileName = "deepseek-models-v1.json"
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

func DeepSeekModelCatalogJSON() []byte {
	raw, err := embeddedDeepSeekModelCatalog.ReadFile("deepseek_models.json")
	if err != nil {
		return nil
	}
	return append([]byte(nil), raw...)
}
