package cloudflaredembed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestEnsureSiblingExtractsEmbeddedAsset(t *testing.T) {
	payload := []byte("cloudflared-test-binary")
	previous, hadPrevious := Current()
	register(Asset{
		Version: "test",
		SHA256:  sha256Hex(payload),
		Zstd:    zstdBytes(t, payload),
	})
	t.Cleanup(func() {
		if hadPrevious {
			register(previous)
			return
		}
		mu.Lock()
		current = Asset{}
		mu.Unlock()
	})

	dir := t.TempDir()
	currentBinary := filepath.Join(dir, executableName("codex-remote"))
	if err := os.WriteFile(currentBinary, []byte("codex-remote"), 0o755); err != nil {
		t.Fatalf("seed current binary: %v", err)
	}

	targetPath, ok, err := EnsureSibling(currentBinary)
	if err != nil {
		t.Fatalf("EnsureSibling: %v", err)
	}
	if !ok {
		t.Fatal("EnsureSibling reported no embedded asset")
	}
	if targetPath != filepath.Join(dir, executableName("cloudflared")) {
		t.Fatalf("targetPath = %q, want sibling path", targetPath)
	}
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read extracted asset: %v", err)
	}
	if string(content) != "cloudflared-test-binary" {
		t.Fatalf("extracted content = %q", string(content))
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat extracted asset: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("extracted asset mode = %v, want executable", info.Mode())
	}
}

func TestEnsureSiblingReusesExistingFile(t *testing.T) {
	payload := []byte("new-binary")
	previous, hadPrevious := Current()
	register(Asset{
		Version: "test",
		SHA256:  sha256Hex(payload),
		Zstd:    zstdBytes(t, payload),
	})
	t.Cleanup(func() {
		if hadPrevious {
			register(previous)
			return
		}
		mu.Lock()
		current = Asset{}
		mu.Unlock()
	})

	dir := t.TempDir()
	currentBinary := filepath.Join(dir, executableName("codex-remote"))
	if err := os.WriteFile(currentBinary, []byte("codex-remote"), 0o755); err != nil {
		t.Fatalf("seed current binary: %v", err)
	}
	existingPath := filepath.Join(dir, executableName("cloudflared"))
	if err := os.WriteFile(existingPath, []byte("existing-binary"), 0o755); err != nil {
		t.Fatalf("seed existing asset: %v", err)
	}

	targetPath, ok, err := EnsureSibling(currentBinary)
	if err != nil {
		t.Fatalf("EnsureSibling: %v", err)
	}
	if !ok {
		t.Fatal("EnsureSibling reported no bundled binary")
	}
	if targetPath != existingPath {
		t.Fatalf("targetPath = %q, want %q", targetPath, existingPath)
	}
	content, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read existing asset: %v", err)
	}
	if string(content) != "existing-binary" {
		t.Fatalf("existing content was overwritten: %q", string(content))
	}
}

func zstdBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer, err := zstd.NewWriter(&buffer)
	if err != nil {
		t.Fatalf("zstd new writer: %v", err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatalf("zstd write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	result := buffer.Bytes()
	if len(result) == 0 {
		t.Fatal("zstd result is empty")
	}
	return result
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
