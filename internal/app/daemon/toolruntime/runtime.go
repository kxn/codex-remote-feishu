package toolruntime

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	ListenAddr string
	StateFile  string
}

type ServiceInfo struct {
	URL         string    `json:"url"`
	Protocol    string    `json:"protocol,omitempty"`
	Transport   string    `json:"transport,omitempty"`
	ManifestURL string    `json:"manifestUrl,omitempty"`
	CallURL     string    `json:"callUrl,omitempty"`
	Token       string    `json:"token"`
	TokenType   string    `json:"tokenType"`
	GeneratedAt time.Time `json:"generatedAt"`
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
	info := ServiceInfo{
		URL:         "http://" + t.Listener.Addr().String(),
		Protocol:    "mcp",
		Transport:   "streamable_http",
		Token:       t.BearerToken,
		TokenType:   "bearer",
		GeneratedAt: time.Now().UTC(),
	}
	return writeJSONFileAtomic(t.StatePath, info, 0o600)
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

func writeJSONFileAtomic(path string, payload any, mode os.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmpFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if err := tmpFile.Chmod(mode); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
