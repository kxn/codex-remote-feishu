package embed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestWriteExecutableExtractsCurrentAsset(t *testing.T) {
	payload := []byte("managed-shim-binary")
	previous, hadPrevious := Current()
	register(Asset{
		SourceDigest: "test",
		SHA256:       sha256Hex(payload),
		Zstd:         zstdBytes(t, payload),
	})
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if hadPrevious {
			current = previous
			return
		}
		current = Asset{}
	})

	target := filepath.Join(t.TempDir(), "codex")
	if err := WriteExecutable(target); err != nil {
		t.Fatalf("WriteExecutable: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) != string(payload) {
		t.Fatalf("payload = %q, want %q", string(raw), string(payload))
	}
}

func zstdBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer, err := zstd.NewWriter(&buffer)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buffer.Bytes()
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
