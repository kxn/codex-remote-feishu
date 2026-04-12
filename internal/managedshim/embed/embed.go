package embed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/klauspost/compress/zstd"
)

type Asset struct {
	SourceDigest string
	SHA256       string
	Zstd         []byte
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

func ExpectedSHA256() string {
	asset, ok := Current()
	if !ok {
		return ""
	}
	return normalizeSHA256(asset.SHA256)
}

func WriteExecutable(path string) error {
	asset, ok := Current()
	if !ok {
		return fmt.Errorf("no embedded managed shim asset for this platform")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), "managed-shim-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanup := func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}

	reader, err := zstd.NewReader(bytes.NewReader(asset.Zstd))
	if err != nil {
		cleanup()
		return err
	}
	defer reader.Close()

	digest := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tempFile, digest), reader); err != nil {
		cleanup()
		return err
	}
	if sum := hex.EncodeToString(digest.Sum(nil)); sum != normalizeSHA256(asset.SHA256) {
		cleanup()
		return fmt.Errorf("managed shim digest mismatch: got %s want %s", sum, normalizeSHA256(asset.SHA256))
	}
	if err := tempFile.Chmod(0o755); err != nil {
		cleanup()
		return err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func normalizeSHA256(value string) string {
	return hex.EncodeToString(mustDecodeSHA256(value))
}

func mustDecodeSHA256(value string) []byte {
	trimmed := value
	if len(trimmed) >= len("sha256:") && trimmed[:len("sha256:")] == "sha256:" {
		trimmed = trimmed[len("sha256:"):]
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil
	}
	return decoded
}
