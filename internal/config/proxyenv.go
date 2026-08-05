package config

import (
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

var ProxyEnvKeys = []string{
	"http_proxy",
	"https_proxy",
	"all_proxy",
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"ALL_PROXY",
}

func CaptureAndClearProxyEnv() []string {
	return captureProxyEnv(true)
}

func CaptureProxyEnv() []string {
	return captureProxyEnv(false)
}

func captureProxyEnv(clear bool) []string {
	var restored []string
	for _, key := range ProxyEnvKeys {
		if value, ok := os.LookupEnv(key); ok {
			restored = append(restored, key+"="+value)
			if clear {
				_ = os.Unsetenv(key)
			}
		}
	}
	return restored
}

func FilterEnvWithoutProxy(env []string) []string {
	filtered := env[:0]
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if xutil.ContainsString(ProxyEnvKeys, key) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
