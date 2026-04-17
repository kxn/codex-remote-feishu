package daemon

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func (a *App) SetToolRuntime(cfg ToolRuntimeConfig) {
	a.toolRuntime.configure(
		cfg,
		a.requireToolAuth(a.handleToolManifest),
		a.requireToolAuth(a.handleToolCall),
	)
}

func (a *App) bindToolListenerLocked() error {
	return a.toolRuntime.bindLocked()
}

func (a *App) removeToolServiceStateLocked() {
	a.toolRuntime.removeStateLocked()
}

func (t *toolRuntimeState) configure(cfg ToolRuntimeConfig, manifestHandler, callHandler http.HandlerFunc) {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /v1/tools/manifest", manifestHandler)
	mux.HandleFunc("POST /v1/tools/call", callHandler)
	t.server = &http.Server{Addr: cfg.ListenAddr, Handler: mux}
	t.statePath = strings.TrimSpace(cfg.StateFile)
}

func (t *toolRuntimeState) bindLocked() error {
	if t.server == nil || t.listener != nil {
		return nil
	}
	listener, err := net.Listen("tcp", t.server.Addr)
	if err != nil {
		return err
	}
	token, err := generateToolBearerToken()
	if err != nil {
		_ = listener.Close()
		return err
	}
	t.listener = listener
	t.bearerToken = token
	if err := t.persistStateLocked(); err != nil {
		_ = listener.Close()
		t.listener = nil
		t.bearerToken = ""
		return err
	}
	return nil
}

func (t *toolRuntimeState) persistStateLocked() error {
	if strings.TrimSpace(t.statePath) == "" || t.listener == nil || strings.TrimSpace(t.bearerToken) == "" {
		return nil
	}
	info := toolServiceInfo{
		URL:         "http://" + t.listener.Addr().String(),
		ManifestURL: "http://" + t.listener.Addr().String() + "/v1/tools/manifest",
		CallURL:     "http://" + t.listener.Addr().String() + "/v1/tools/call",
		Token:       t.bearerToken,
		TokenType:   "bearer",
		GeneratedAt: time.Now().UTC(),
	}
	return writeJSONFileAtomic(t.statePath, info, 0o600)
}

func (t *toolRuntimeState) removeStateLocked() {
	if strings.TrimSpace(t.statePath) == "" {
		return
	}
	if err := os.Remove(t.statePath); err != nil && !os.IsNotExist(err) {
		log.Printf("remove tool service state failed: path=%s err=%v", t.statePath, err)
	}
}

func generateToolBearerToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
