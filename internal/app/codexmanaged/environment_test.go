package codexmanaged

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRegistryProxyUsesSuppliedEnvAndHonorsBypass(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://global.invalid:1234")
	req, err := http.NewRequest(http.MethodGet, registryURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		env      []string
		expected string
	}{
		{"no global fallback", nil, ""},
		{"upper wins", []string{"https_proxy=http://lower:1", "ALL_PROXY=http://all:2", "HTTPS_PROXY=http://upper:3"}, "http://upper:3"},
		{"lowercase", []string{"https_proxy=http://lower:1", "ALL_PROXY=http://all:2"}, "http://lower:1"},
		{"all", []string{"all_proxy=socks5://all:2"}, "socks5://all:2"},
		{"http fallback", []string{"HTTP_PROXY=http://fallback:4"}, "http://fallback:4"},
		{"domain bypass", []string{"HTTPS_PROXY=http://proxy:3", "no_proxy=localhost,.npmjs.org"}, ""},
		{"port bypass", []string{"HTTPS_PROXY=http://proxy:3", "NO_PROXY=registry.npmjs.org:443"}, ""},
		{"other port", []string{"HTTPS_PROXY=http://proxy:3", "NO_PROXY=registry.npmjs.org:80"}, "http://proxy:3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proxy, err := proxyFromEnv(tc.env)(req)
			if err != nil {
				t.Fatal(err)
			}
			actual := ""
			if proxy != nil {
				actual = proxy.String()
			}
			if actual != tc.expected {
				t.Fatalf("proxy=%s expected=%s", actual, tc.expected)
			}
		})
	}
	_, err = proxyFromEnv([]string{"HTTPS_PROXY=http://secret:password@%zz"})(req)
	if err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe proxy error: %v", err)
	}
}

func TestRegistryLookupActuallyUsesSuppliedProxy(t *testing.T) {
	hits := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.Method + " " + r.Host
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", "http://unavailable.invalid:1")
	if _, err := lookupLatestWithEnv(context.Background(), []string{"HTTPS_PROXY=" + proxy.URL}); err == nil {
		t.Fatal("expected controlled proxy error")
	}
	select {
	case actual := <-hits:
		if actual != "CONNECT registry.npmjs.org:443" {
			t.Fatalf("unexpected request %q", actual)
		}
	default:
		t.Fatal("supplied proxy was not contacted")
	}
}

func TestPrivateCommandsUseSuppliedPathAndEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools are Unix-only")
	}
	tools := t.TempDir()
	argsPath := filepath.Join(tools, "args")
	for name, script := range map[string]string{
		"codex": "#!/bin/sh\n[ \"$CODEX_HOME\" = \"/captured/codex-home\" ] || exit 2\n[ \"$HTTPS_PROXY\" = \"http://captured-proxy:3\" ] || exit 3\nprintf 'codex-cli 0.159.3\\n'\n",
		"npm":   "#!/bin/sh\n[ \"$CODEX_HOME\" = \"/captured/codex-home\" ] || exit 2\n[ \"$HTTPS_PROXY\" = \"http://captured-proxy:3\" ] || exit 3\nprintf '%s\\n' \"$@\" > \"$CODEX_MANAGED_TEST_ARGS\"\n",
	} {
		if err := os.WriteFile(filepath.Join(tools, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"PATH=" + tools, "CODEX_HOME=/captured/codex-home", "HTTPS_PROXY=http://captured-proxy:3", "CODEX_MANAGED_TEST_ARGS=" + argsPath}
	t.Setenv("PATH", "/unavailable-global-path")
	t.Setenv("CODEX_HOME", "/global-codex-home")
	actual, err := readBinaryVersionWithEnv(context.Background(), "codex", env)
	if err != nil || actual != "0.159.3" {
		t.Fatalf("version=%s err=%v", actual, err)
	}
	if err := installPrivateWithEnv(context.Background(), t.TempDir(), "0.159.3", env); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(argsPath); err != nil || !strings.Contains(string(raw), "@openai/codex@0.159.3") {
		t.Fatalf("args=%q err=%v", raw, err)
	}
	if os.Getenv("CODEX_HOME") != "/global-codex-home" {
		t.Fatal("global environment changed")
	}
}

func TestVersionOutputLimitCannotBeBypassedByReaderFrom(t *testing.T) {
	var output versionOutput
	if _, err := io.Copy(&output, strings.NewReader(strings.Repeat("x", 8192))); err != nil {
		t.Fatal(err)
	}
	if !output.exceeded || len(output.String()) != 1024 {
		t.Fatalf("output=%d exceeded=%v", len(output.String()), output.exceeded)
	}
}

func TestNoProxyCIDRAndWildcard(t *testing.T) {
	for _, item := range []string{"*", "127.0.0.0/8", "127.0.0.1:443"} {
		target, _ := url.Parse("https://127.0.0.1/")
		if !bypassProxy(target, item) {
			t.Fatalf("did not bypass %s", item)
		}
	}
}
