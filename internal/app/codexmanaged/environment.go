package codexmanaged

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func envValue(env []string, names ...string) string {
	for _, name := range names {
		for _, entry := range env {
			key, value, ok := strings.Cut(entry, "=")
			if ok && key == name && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func proxyFromEnv(env []string) func(*http.Request) (*url.URL, error) {
	proxy := envValue(env, "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy")
	noProxy := envValue(env, "NO_PROXY", "no_proxy")
	return func(req *http.Request) (*url.URL, error) {
		if proxy == "" || bypassProxy(req.URL, noProxy) {
			return nil, nil
		}
		parsed, err := url.Parse(proxy)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5" && parsed.Scheme != "socks5h") {
			return nil, fmt.Errorf("invalid Codex registry proxy configuration")
		}
		return parsed, nil
	}
}

func bypassProxy(target *url.URL, list string) bool {
	host := strings.ToLower(target.Hostname())
	ip := net.ParseIP(host)
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	for _, item := range strings.Split(list, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "*" {
			return true
		}
		if _, network, err := net.ParseCIDR(item); err == nil && ip != nil && network.Contains(ip) {
			return true
		}
		if bypassHost, bypassPort, err := net.SplitHostPort(item); err == nil {
			if port != bypassPort {
				continue
			}
			item = bypassHost
		}
		item = strings.TrimPrefix(strings.TrimPrefix(item, "*"), ".")
		if item != "" && (host == item || strings.HasSuffix(host, "."+item)) {
			return true
		}
	}
	return false
}

// exec.LookPath reads the process PATH before cmd.Env can be assigned. Resolve
// from the supplied environment first, so restored child PATH is respected too.
func executableFromEnv(name string, env []string) (string, error) {
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return name, nil
	}
	path := envValue(env, "PATH", "Path", "path")
	extensions := []string{""}
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		ext := envValue(env, "PATHEXT", "Pathext")
		if ext == "" {
			ext = ".COM;.EXE;.BAT;.CMD"
		}
		extensions = append(extensions, strings.Split(strings.ToLower(ext), ";")...)
	}
	for _, directory := range filepath.SplitList(path) {
		if directory == "" {
			directory = "."
		}
		for _, extension := range extensions {
			candidate := filepath.Join(directory, name+extension)
			info, err := os.Stat(candidate)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
				continue
			}
			return filepath.Abs(candidate)
		}
	}
	return "", fmt.Errorf("resolve %s in supplied PATH: %w", name, exec.ErrNotFound)
}
