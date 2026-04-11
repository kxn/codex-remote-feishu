package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAppConfigLeavesPprofDisabledByDefault(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")

	if err := WriteAppConfig(configPath, DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	loaded, err := LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Debug.Pprof != nil {
		t.Fatalf("expected pprof to stay disabled by default, got %#v", loaded.Config.Debug.Pprof)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "\"pprof\"") {
		t.Fatalf("expected default config to omit pprof, got %s", raw)
	}
}

func TestWriteAppConfigNormalizesEnabledPprofDefaults(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultAppConfig()
	cfg.Debug.Pprof = &PprofSettings{Enabled: true}

	if err := WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	loaded, err := LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Debug.Pprof == nil {
		t.Fatal("expected pprof config to be present")
	}
	if !loaded.Config.Debug.Pprof.Enabled {
		t.Fatalf("expected pprof to be enabled, got %#v", loaded.Config.Debug.Pprof)
	}
	if loaded.Config.Debug.Pprof.ListenHost != "127.0.0.1" {
		t.Fatalf("ListenHost = %q, want 127.0.0.1", loaded.Config.Debug.Pprof.ListenHost)
	}
	if loaded.Config.Debug.Pprof.ListenPort != 17501 {
		t.Fatalf("ListenPort = %d, want 17501", loaded.Config.Debug.Pprof.ListenPort)
	}
}
