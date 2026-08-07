package toolruntime

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/toolservicecontract"
)

type Config struct {
	ListenAddr string
	StateFile  string
}

type State struct {
	Server      *http.Server
	Listener    net.Listener
	StatePath   string
	BearerToken string
}

func (t *State) Configure(cfg Config, toolHandler http.Handler) {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/", toolHandler)
	t.Server = &http.Server{Addr: cfg.ListenAddr, Handler: mux}
	t.StatePath = strings.TrimSpace(cfg.StateFile)
}

func (t *State) BindLocked() error {
	if t.Server == nil || t.Listener != nil {
		return nil
	}
	listener, err := net.Listen("tcp", t.Server.Addr)
	if err != nil {
		return err
	}
	token, err := generateBearerToken()
	if err != nil {
		_ = listener.Close()
		return err
	}
	t.Listener = listener
	t.BearerToken = token
	if err := t.persistStateLocked(); err != nil {
		_ = listener.Close()
		t.Listener = nil
		t.BearerToken = ""
		return err
	}
	return nil
}

func (t *State) persistStateLocked() error {
	if strings.TrimSpace(t.StatePath) == "" || t.Listener == nil || strings.TrimSpace(t.BearerToken) == "" {
		return nil
	}
	info := toolservicecontract.ServiceInfo{
		URL:         "http://" + t.Listener.Addr().String(),
		Protocol:    "mcp",
		Transport:   "streamable_http",
		Token:       t.BearerToken,
		TokenType:   "bearer",
		GeneratedAt: time.Now().UTC(),
	}
	return toolservicecontract.WriteJSONFileAtomic(t.StatePath, info, 0o600)
}

func (t *State) RemoveStateLocked() {
	if strings.TrimSpace(t.StatePath) == "" {
		return
	}
	if err := os.Remove(t.StatePath); err != nil && !os.IsNotExist(err) {
		log.Printf("remove tool service state failed: path=%s err=%v", t.StatePath, err)
	}
}

func generateBearerToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
