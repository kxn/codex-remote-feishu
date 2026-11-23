package wrapper

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

func newPredictionHTTPClient() *http.Client {
	if runtime.GOOS != "darwin" {
		return &http.Client{}
	}
	return predictionHTTPClientWithProxy(macOSPredictionProxy)
}

func predictionHTTPClientWithProxy(resolve func(context.Context, string) *url.URL) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		return resolve(req.Context(), req.URL.Scheme+"://"+req.URL.Host), nil
	}
	return &http.Client{Transport: transport}
}

// Use macOS's route lookup so system exclusions are evaluated for the
// official origin. This runs a read-only query without starting a GUI app.
const macOSPredictionProxyScript = `
ObjC.import('Foundation');
ObjC.import('CFNetwork');
ObjC.bindFunction('CFNetworkCopySystemProxySettings', ['id', []]);
ObjC.bindFunction('CFNetworkCopyProxiesForURL', ['id', ['id', 'id']]);
function run(args) {
    const settings = $.CFNetworkCopySystemProxySettings();
    const url = $.NSURL.URLWithString(args[0]);
    const proxies = ObjC.deepUnwrap($.CFNetworkCopyProxiesForURL(url, settings));
    return JSON.stringify(proxies.map(proxy => ({
        type: proxy.kCFProxyTypeKey,
        host: proxy.kCFProxyHostNameKey || "",
        port: proxy.kCFProxyPortNumberKey || 0
    })));
}
`

func macOSPredictionProxy(ctx context.Context, origin string) *url.URL {
	endpoint, err := url.Parse(origin)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" {
		return nil
	}
	host := endpoint.Hostname()
	if strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-", origin)
	cmd.Stdin = strings.NewReader(macOSPredictionProxyScript)
	raw, err := cmd.Output()
	if err != nil {
		return nil
	}
	return decodeMacOSPredictionProxy(raw)
}

func decodeMacOSPredictionProxy(raw []byte) *url.URL {
	var proxies []struct {
		Type, Host string
		Port       int
	}
	if json.Unmarshal(raw, &proxies) != nil || len(proxies) == 0 {
		return nil
	}

	// The first entry is the system's preferred route. Direct and automatic
	// configuration entries do not describe a fixed proxy endpoint.
	proxy := proxies[0]
	scheme := ""
	switch proxy.Type {
	case "kCFProxyTypeHTTP", "kCFProxyTypeHTTPS":
		scheme = "http"
	case "kCFProxyTypeSOCKS":
		scheme = "socks5"
	}
	if scheme == "" || proxy.Host == "" || proxy.Port < 1 || proxy.Port > 65535 || strings.ContainsAny(proxy.Host, "/@?#") {
		return nil
	}
	return &url.URL{Scheme: scheme, Host: net.JoinHostPort(proxy.Host, strconv.Itoa(proxy.Port))}
}
