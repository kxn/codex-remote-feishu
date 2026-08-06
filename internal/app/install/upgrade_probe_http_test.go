package install

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbeHTTPGetStatusAndJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/api/admin/bootstrap-state":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"setupRequired":false,"gateways":[{"state":"connected"}]}`))
		case "/v1/status":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()

	status, _, err := probeHTTPGet(ctx, server.URL+"/healthz")
	if err != nil {
		t.Fatalf("probeHTTPGet healthz: %v", err)
	}
	if status != 200 {
		t.Fatalf("healthz status = %d, want 200", status)
	}

	status, body, err := probeHTTPGet(ctx, server.URL+"/api/admin/bootstrap-state")
	if err != nil {
		t.Fatalf("probeHTTPGet bootstrap-state: %v", err)
	}
	if status != 200 {
		t.Fatalf("bootstrap-state status = %d, want 200", status)
	}
	var state upgradeHelperBootstrapState
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("unmarshal bootstrap-state: %v", err)
	}
	if state.SetupRequired || len(state.Gateways) != 1 || state.Gateways[0].State != "connected" {
		t.Fatalf("bootstrap-state = %+v, want setupRequired=false gateways=[connected]", state)
	}
}

func TestProbeHTTPGetNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	status, _, err := probeHTTPGet(context.Background(), server.URL+"/nope")
	if err != nil {
		t.Fatalf("probeHTTPGet: %v", err)
	}
	if status != 404 {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestProbeHTTPGetChunked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}
		// Write in two chunks so the server uses chunked transfer encoding.
		_, _ = w.Write([]byte(`{"setupRequired":`))
		flusher.Flush()
		_, _ = w.Write([]byte(`true}`))
		flusher.Flush()
	}))
	defer server.Close()

	status, body, err := probeHTTPGet(context.Background(), server.URL+"/chunked")
	if err != nil {
		t.Fatalf("probeHTTPGet chunked: %v", err)
	}
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	var state upgradeHelperBootstrapState
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("unmarshal chunked body %q: %v", string(body), err)
	}
	if !state.SetupRequired {
		t.Fatalf("chunked body = %+v, want setupRequired=true", state)
	}
}

func TestProbeHTTPGetRejectsHTTPS(t *testing.T) {
	_, _, err := probeHTTPGet(context.Background(), "https://127.0.0.1/healthz")
	if err == nil || !strings.Contains(err.Error(), "must use http scheme") {
		t.Fatalf("https probe error = %v, want scheme rejection", err)
	}
}

func TestProbeHTTPGetTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := probeHTTPGet(ctx, server.URL+"/slow")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout probe took %v, want ~100ms", elapsed)
	}
}

func TestFetchJSONAndExpectHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"gateways":[]}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	if err := expectHTTPStatus(ctx, server.URL+"/ok", 200); err != nil {
		t.Fatalf("expectHTTPStatus ok: %v", err)
	}
	if err := expectHTTPStatus(ctx, server.URL+"/err", 200); err == nil {
		t.Fatal("expectHTTPStatus err: expected error for 500")
	}
	var state upgradeHelperBootstrapState
	if err := fetchJSON(ctx, server.URL+"/json", &state); err != nil {
		t.Fatalf("fetchJSON: %v", err)
	}
	if len(state.Gateways) != 0 {
		t.Fatalf("fetchJSON gateways = %+v, want empty", state.Gateways)
	}
}
