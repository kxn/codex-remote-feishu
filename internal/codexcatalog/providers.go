package codexcatalog

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

// EmbeddedCatalog 描述一个内置模型目录的 provider：端点集合 + 模型名前缀
// 决定识别（两者独立触发，任一命中即视为该 provider），FileName 决定注入
// 的目录文件名，FallbackSlug 是未知模型的元数据模板。
//
// 识别是互斥的单选：一个 profile 只会命中一个 catalog，因此注入的模型目录
// 只包含该 provider 的模型（如 mimo 端点/模型名下不会出现 deepseek 模型）。
type EmbeddedCatalog struct {
	Kind         string
	ModelPrefix  string
	Endpoints    []string
	FileName     string
	FallbackSlug string
	// EmbedFile 是 go:embed 源文件名（磁盘上的 json 资源），FileName 是
	// 注入到状态目录时的输出文件名，两者可以不同。
	EmbedFile string
}

// DeepSeekCatalog 是 DeepSeek 官方端点的内置模型目录。
var DeepSeekCatalog = EmbeddedCatalog{
	Kind:         "deepseek",
	ModelPrefix:  "deepseek-",
	Endpoints:    []string{"api.deepseek.com"},
	FileName:     DeepSeekModelCatalogFileName,
	FallbackSlug: "deepseek-v4-flash",
	EmbedFile:    "deepseek_models.json",
}

// MimoCatalog 是小米 MiMo（按量付费 + Token Plan）的内置模型目录。
// 端点与模型名前缀均来自 MiMo Codex 配置文档（mimo.mi.com）。
var MimoCatalog = EmbeddedCatalog{
	Kind:         "mimo",
	ModelPrefix:  "mimo-",
	Endpoints:    []string{"api.xiaomimimo.com", "token-plan-cn.xiaomimimo.com"},
	FileName:     MimoModelCatalogFileName,
	FallbackSlug: "mimo-v2.5",
	EmbedFile:    "mimo_models.json",
}

var embeddedCatalogs = []EmbeddedCatalog{DeepSeekCatalog, MimoCatalog}

// IdentifyEmbeddedCatalog 识别 baseURL/模型名命中的内置模型目录。
// 模型名前缀优先（覆盖 sub2api 中转等任意端点场景），端点兜底（覆盖
// 模型名未知但端点明确是官方网关的场景）。未命中返回 (零值, false)。
func IdentifyEmbeddedCatalog(baseURL, model string) (EmbeddedCatalog, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, catalog := range embeddedCatalogs {
		if catalog.ModelPrefix != "" && strings.HasPrefix(model, catalog.ModelPrefix) {
			return catalog, true
		}
	}
	for _, catalog := range embeddedCatalogs {
		if catalog.MatchesEndpoint(baseURL) {
			return catalog, true
		}
	}
	return EmbeddedCatalog{}, false
}

// MatchesEndpoint 报告 baseURL 的 host 是否属于该 catalog 的官方端点。
func (c EmbeddedCatalog) MatchesEndpoint(baseURL string) bool {
	host := endpointHost(baseURL)
	for _, endpoint := range c.Endpoints {
		if host == endpoint {
			return true
		}
	}
	return false
}

// CatalogPath 返回该 catalog 在管理目录下的状态文件名。
func (c EmbeddedCatalog) CatalogPath(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, c.FileName)
}

// CatalogJSON 返回该 catalog 的内嵌目录内容（防御性副本）。
func (c EmbeddedCatalog) CatalogJSON() []byte {
	raw, err := embeddedCatalogsFS.ReadFile(c.EmbedFile)
	if err != nil {
		return nil
	}
	return append([]byte(nil), raw...)
}

// endpointHost 解析 baseURL 的 hostname（小写、去端口），解析失败返回空串。
func endpointHost(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		parsed, err = url.Parse("https://" + strings.TrimLeft(baseURL, "/"))
	}
	if err != nil || parsed.Host == "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		if splitHost, _, err := net.SplitHostPort(parsed.Host); err == nil {
			host = strings.ToLower(strings.TrimSpace(splitHost))
		}
	}
	return host
}
