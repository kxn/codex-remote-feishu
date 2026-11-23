package wrapper

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPredictionBackendRequestUsesSystemProxyForBothOfficialCalls(t *testing.T) {
	calls := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls <- r.Method + " " + r.Host
		if r.Header.Get("Authorization") != "" {
			t.Error("native bearer was exposed in the CONNECT request")
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	proxy, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	p := newTestPredictionRuntime()
	p.httpClient = predictionHTTPClientWithProxy(func(_ context.Context, origin string) (*url.URL, error) {
		if origin != "https://chatgpt.com" {
			t.Errorf("proxy received unexpected route %q", origin)
		}
		return proxy, nil
	})
	defer p.httpClient.CloseIdleConnections()
	routing := predictionRouting{BackendOrigin: "https://chatgpt.com"}
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/wham/predictions/config"},
		{http.MethodPost, "/wham/statsig/bootstrap"},
	} {
		if _, err := p.predictionBackendRequest(context.Background(), "native-token", predictionIdentity{}, routing, request.method, request.path, nil); err == nil {
			t.Fatal("expected the CONNECT refusal")
		}
		select {
		case call := <-calls:
			if call != "CONNECT chatgpt.com:443" {
				t.Fatalf("got %s", call)
			}
		default:
			t.Fatal("request did not reach the system proxy")
		}
	}
}

func TestDecodeMacOSPredictionProxyUsesPreferredRoute(t *testing.T) {
	for _, tc := range []struct {
		name, response, want string
		wantError            bool
	}{
		{"HTTPS CONNECT", `[{"type":"kCFProxyTypeHTTPS","host":"127.0.0.1","port":33210}]`, "http://127.0.0.1:33210", false},
		{"SOCKS IPv6", `[{"type":"kCFProxyTypeSOCKS","host":"::1","port":33211}]`, "socks5://[::1]:33211", false},
		{"excluded", `[{"type":"kCFProxyTypeNone"},{"type":"kCFProxyTypeHTTP","host":"host","port":80}]`, "", false},
		{"PAC URL", `[{"type":"kCFProxyTypeAutoConfigurationURL"},{"type":"kCFProxyTypeNone"}]`, "", true},
		{"PAC JavaScript", `[{"type":"kCFProxyTypeAutoConfigurationJavaScript"}]`, "", true},
		{"unsupported type", `[{"type":"future-route-type"}]`, "", true},
		{"invalid endpoint", `[{"type":"kCFProxyTypeHTTPS","host":"host","port":65536}]`, "", true},
		{"empty", `[]`, "", true},
		{"malformed", `invalid`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxy, err := decodeMacOSPredictionProxy([]byte(tc.response))
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected route resolution error: %v", err)
			}
			got := ""
			if proxy != nil {
				got = proxy.String()
			}
			if got != tc.want {
				t.Fatalf("route = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPredictionHTTPClientDoesNotDialWhenSystemProxyUnresolved(t *testing.T) {
	client := predictionHTTPClientWithProxy(func(context.Context, string) (*url.URL, error) {
		return decodeMacOSPredictionProxy([]byte(`[{"type":"kCFProxyTypeAutoConfigurationURL"}]`))
	})
	defer client.CloseIdleConnections()
	client.Transport.(*http.Transport).DialContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("unresolved proxy caused a connection attempt")
		return nil, errors.New("unexpected network request")
	}
	_, err := client.Get("https://chatgpt.com/wham/predictions/config")
	if !errors.Is(err, errPredictionSystemProxyUnresolved) {
		t.Fatalf("expected unresolved proxy error, got %v", err)
	}
}
