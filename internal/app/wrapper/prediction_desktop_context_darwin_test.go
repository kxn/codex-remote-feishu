//go:build darwin

package wrapper

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func desktopContextFixture(t *testing.T) (string, string, time.Time) {
	t.Helper()
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "ChatGPT.app")
	for _, dir := range []string{filepath.Join(bundle, "Contents", "Resources"), filepath.Join(home, "Library", "Application Support", "Codex", "Default")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.openai.codex</string><key>CFBundleDisplayName</key><string>ChatGPT</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0600); err != nil {
		t.Fatal(err)
	}
	writeDesktopASAR(t, bundle, `{"version":"26.test","codexBuildFlavor":"prod"}`)
	base := filepath.Join(home, "Library", "Application Support", "Codex")
	for path, contents := range map[string]string{
		filepath.Join(base, "statsig-state.json"):     `{"statsig-stable-id":"fixture-stable"}`,
		filepath.Join(base, "Default", "Preferences"): `{"intl":{"selected_languages":"zh-CN,zh"}}`,
	} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return home, bundle, time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
}

func writeDesktopASAR(t *testing.T, bundle, pkg string) {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"files": map[string]any{"package.json": map[string]any{"offset": "4096", "size": len(pkg)}}})
	padded := (len(header) + 3) &^ 3
	data := make([]byte, 16+padded)
	binary.LittleEndian.PutUint32(data, 4)
	binary.LittleEndian.PutUint32(data[4:], uint32(padded+8))
	binary.LittleEndian.PutUint32(data[8:], uint32(padded+4))
	binary.LittleEndian.PutUint32(data[12:], uint32(len(header)))
	copy(data[16:], header)
	f, err := os.Create(filepath.Join(bundle, "Contents", "Resources", "app.asar"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte(pkg), int64(len(data)+4096)); err != nil {
		t.Fatal(err)
	}
	// A sparse unrelated tail ensures the reader needs only the indexed entry.
	if err = f.Truncate(128 << 20); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopContextMetadataAndNewestSession(t *testing.T) {
	home, bundle, now := desktopContextFixture(t)
	logs := filepath.Join(home, "Library", "Logs", "com.openai.codex", now.Format("2006/01/02"))
	if err := os.MkdirAll(logs, 0700); err != nil {
		t.Fatal(err)
	}
	ids := []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"}
	for i, id := range ids {
		path := filepath.Join(logs, "codex-desktop-"+id+"-123-t0-i1-000000-0.log")
		if err := os.WriteFile(path, []byte("contents must not matter"), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readPredictionDesktopContextAt(context.Background(), home, []string{bundle}, now, "26.test")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"app_version": "26.test", "build_flavor": "prod", "brand_name": "chatgpt", "locale": "zh-CN", "stable_id": "fixture-stable", "system_name": "macOS", "system_version": "26.test", "window_type": "electron", "app_session_id": ids[1]}
	if len(got) != len(want) {
		t.Fatalf("unexpected context keys: %d", len(got))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s mismatch", k)
		}
	}
}

func TestDesktopContextOptionalSessionAndBundleFallback(t *testing.T) {
	home, bundle, now := desktopContextFixture(t)
	got, err := readPredictionDesktopContextAt(context.Background(), home, []string{filepath.Join(t.TempDir(), "Missing.app"), bundle}, now, "26.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["app_session_id"]; ok {
		t.Fatal("session must be omitted without today's logs")
	}
	if _, ok := got["desktop_app_beta_enabled"]; ok {
		t.Fatal("beta must not be guessed")
	}
}

func TestDesktopContextRejectsUntrustedOrIncompleteMetadata(t *testing.T) {
	for _, test := range []string{"bundle-id", "header", "package", "stable", "locale"} {
		t.Run(test, func(t *testing.T) {
			home, bundle, now := desktopContextFixture(t)
			base := filepath.Join(home, "Library", "Application Support", "Codex")
			switch test {
			case "bundle-id":
				os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(`<plist><dict><key>CFBundleIdentifier</key><string>other.app</string></dict></plist>`), 0600)
			case "header":
				os.WriteFile(filepath.Join(bundle, "Contents", "Resources", "app.asar"), make([]byte, 16), 0600)
			case "package":
				writeDesktopASAR(t, bundle, `{"version":"26.test"}`)
			case "stable":
				os.WriteFile(filepath.Join(base, "statsig-state.json"), []byte("{}"), 0600)
			case "locale":
				os.WriteFile(filepath.Join(base, "Default", "Preferences"), []byte("{}"), 0600)
			}
			if _, err := readPredictionDesktopContextAt(context.Background(), home, []string{bundle}, now, "26.test"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDesktopContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readPredictionDesktopContextAt(ctx, "", nil, time.Now(), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
