package cloudflaredembed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

type Asset struct {
	Version string
	SHA256  string
	Zstd    []byte
}

var (
	mu      sync.RWMutex
	current Asset
)

func register(asset Asset) {
	if len(asset.Zstd) == 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	current = asset
}

func Current() (Asset, bool) {
	mu.RLock()
	defer mu.RUnlock()
	if len(current.Zstd) == 0 {
		return Asset{}, false
	}
	return current, true
}

func EnsureSibling(currentBinary string) (string, bool, error) {
	currentBinary = strings.TrimSpace(currentBinary)
	if currentBinary == "" {
		return "", false, nil
	}
	targetPath := filepath.Join(filepath.Dir(currentBinary), executableName("cloudflared"))
	if info, err := os.Stat(targetPath); err == nil && info.Mode().IsRegular() {
		return targetPath, true, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, fmt.Errorf("stat bundled cloudflared: %w", err)
	}

	asset, ok := Current()
	if !ok {
		return "", false, nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", false, fmt.Errorf("create bundled tool dir: %w", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), "cloudflared-*.tmp")
	if err != nil {
		return "", false, fmt.Errorf("create bundled cloudflared temp file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}

	zstdReader, err := zstd.NewReader(bytes.NewReader(asset.Zstd))
	if err != nil {
		cleanupTemp()
		return "", false, fmt.Errorf("open embedded cloudflared asset: %w", err)
	}
	defer zstdReader.Close()

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tempFile, hash), zstdReader); err != nil {
		cleanupTemp()
		return "", false, fmt.Errorf("extract embedded cloudflared asset: %w", err)
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if want := normalizeSHA256(asset.SHA256); want != "" && sum != want {
		cleanupTemp()
		return "", false, fmt.Errorf("embedded cloudflared digest mismatch: got %s want %s", sum, want)
	}
	if err := tempFile.Chmod(0o755); err != nil {
		cleanupTemp()
		return "", false, fmt.Errorf("chmod embedded cloudflared asset: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", false, fmt.Errorf("close bundled cloudflared temp file: %w", err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		if info, statErr := os.Stat(targetPath); statErr == nil && info.Mode().IsRegular() {
			_ = os.Remove(tempPath)
			return targetPath, true, nil
		}
		_ = os.Remove(tempPath)
		return "", false, fmt.Errorf("install bundled cloudflared: %w", err)
	}
	return targetPath, true, nil
}

func normalizeSHA256(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "sha256:"))
	return strings.ToLower(value)
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
