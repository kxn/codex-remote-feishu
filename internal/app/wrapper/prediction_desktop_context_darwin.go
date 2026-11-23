//go:build darwin

package wrapper

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

// Beta is intentionally omitted: a stored preference alone cannot establish the
// effective desktop gate and enterprise policy. The authenticated bootstrap is
// responsible for evaluating controls; this loader reads only local metadata.
func readPredictionDesktopContext(ctx context.Context, accountID string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	version, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return nil, err
	}
	return readPredictionDesktopContextAt(ctx, home, []string{"/Applications/ChatGPT.app", "/Applications/Codex.app"}, time.Now().UTC(), version)
}

func readPredictionDesktopContextAt(ctx context.Context, home string, bundles []string, now time.Time, systemVersion string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var brand, bundle string
	for _, candidate := range bundles {
		plist := filepath.Join(candidate, "Contents", "Info.plist")
		if _, err := os.Stat(plist); errors.Is(err, os.ErrNotExist) {
			continue
		}
		raw, err := execlaunch.CommandContext(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", plist).Output()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("read desktop bundle metadata: %w", err)
		}
		var info struct {
			ID          string `json:"CFBundleIdentifier"`
			Name        string `json:"CFBundleName"`
			DisplayName string `json:"CFBundleDisplayName"`
		}
		if err = json.Unmarshal(raw, &info); err != nil {
			return nil, err
		}
		if info.ID != "com.openai.codex" {
			continue
		}
		brand = info.DisplayName
		if brand == "" {
			brand = info.Name
		}
		bundle = candidate
		break
	}
	if bundle == "" || strings.TrimSpace(brand) == "" {
		return nil, errors.New("installed Codex desktop bundle unavailable")
	}
	pkg, err := readDesktopASARPackage(filepath.Join(bundle, "Contents", "Resources", "app.asar"))
	if err != nil {
		return nil, fmt.Errorf("read desktop package metadata: %w", err)
	}
	var metadata struct {
		Version string `json:"version"`
		Flavor  string `json:"codexBuildFlavor"`
	}
	if err = json.Unmarshal(pkg, &metadata); err != nil {
		return nil, err
	}
	base := filepath.Join(home, "Library", "Application Support", "Codex")
	var state map[string]string
	raw, err := os.ReadFile(filepath.Join(base, "statsig-state.json"))
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	var preferences struct {
		Intl struct {
			Languages string `json:"selected_languages"`
		} `json:"intl"`
	}
	raw, err = os.ReadFile(filepath.Join(base, "Default", "Preferences"))
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &preferences); err != nil {
		return nil, err
	}
	locale, _, _ := strings.Cut(preferences.Intl.Languages, ",")
	locale = strings.TrimSpace(locale)
	stableID := state["statsig-stable-id"]
	if metadata.Version == "" || metadata.Flavor == "" || stableID == "" || locale == "" || systemVersion == "" {
		return nil, errors.New("desktop prediction context is incomplete")
	}
	result := map[string]any{
		"app_version": metadata.Version, "build_flavor": metadata.Flavor, "brand_name": strings.ToLower(brand),
		"locale": locale, "stable_id": stableID, "system_name": "macOS", "system_version": systemVersion, "window_type": "electron",
	}
	if session := desktopSessionFromLogNames(filepath.Join(home, "Library", "Logs", "com.openai.codex", now.Format("2006/01/02"))); session != "" {
		result["app_session_id"] = session
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ASAR offsets are relative to the data section after the header pickle. Read
// only its bounded index and the root package entry, never the complete archive.
func readDesktopASARPackage(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var prefix [16]byte
	if _, err = io.ReadFull(f, prefix[:]); err != nil {
		return nil, err
	}
	size := int64(binary.LittleEndian.Uint32(prefix[4:8]))
	length := int64(binary.LittleEndian.Uint32(prefix[12:16]))
	if binary.LittleEndian.Uint32(prefix[:4]) != 4 || length < 2 || length > 16<<20 || size < 8+length || size > 16<<20+8 {
		return nil, errors.New("invalid desktop ASAR header")
	}
	raw := make([]byte, length)
	if _, err = io.ReadFull(f, raw); err != nil {
		return nil, err
	}
	var index struct {
		Files map[string]struct {
			Offset   string `json:"offset"`
			Size     int64  `json:"size"`
			Unpacked bool   `json:"unpacked"`
		} `json:"files"`
	}
	if err = json.Unmarshal(raw, &index); err != nil {
		return nil, err
	}
	entry, ok := index.Files["package.json"]
	offset, parseErr := strconv.ParseInt(entry.Offset, 10, 64)
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := 8 + size
	if !ok || entry.Unpacked || parseErr != nil || offset < 0 || entry.Size <= 0 || entry.Size > 1<<20 || start > stat.Size() || offset > stat.Size()-start || entry.Size > stat.Size()-start-offset {
		return nil, errors.New("invalid desktop ASAR package entry")
	}
	raw = make([]byte, entry.Size)
	if _, err = f.ReadAt(raw, start+offset); err != nil {
		return nil, err
	}
	return raw, nil
}

var desktopLogSessionPattern = regexp.MustCompile(`^codex-desktop-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})-[0-9]+-.*\.log$`)

// The native logger includes the process session UUID in its filename. Never
// inspect log contents; today's latest matching file is only an optional hint.
func desktopSessionFromLogNames(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var latest time.Time
	var session string
	for _, entry := range entries {
		match := desktopLogSessionPattern.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if session == "" || info.ModTime().After(latest) {
			latest = info.ModTime()
			session = match[1]
		}
	}
	return session
}
