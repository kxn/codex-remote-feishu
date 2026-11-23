package privatestaging

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestOwnershipAndCleanup(t *testing.T) {
	base := t.TempDir()
	owner := Owner{"tenant", "app", "gateway", "event", "message"}
	s, e := Open(base, owner)
	if e != nil {
		t.Fatal(e)
	}
	path, e := s.Create(strings.NewReader("download"), ".txt")
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256([]byte("download"))
	expected := hex.EncodeToString(hash[:])
	if got, err := ExpectedHash(base, owner, path); err != nil || got != expected {
		t.Fatalf("manifest hash: %q %v", got, err)
	}
	reopened, e := Open(base, owner)
	if e != nil || len(reopened.manifest.Files) != 1 {
		t.Fatalf("reopen: %#v %v", reopened, e)
	}
	if e = Cleanup(base, owner, path, "wrong"); e == nil {
		t.Fatal("wrong hash accepted")
	}
	if e = Cleanup(base, owner, filepath.Join(base, "user.txt"), expected); e == nil {
		t.Fatal("foreign path accepted")
	}
	if e = Cleanup(base, owner, path, expected); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("file retained")
	}
}

type brokenDownload struct{}

func (brokenDownload) Read(p []byte) (int, error) {
	copy(p, "partial")
	return len("partial"), io.ErrUnexpectedEOF
}
func TestInterruptedDownloadIsReconciledByRecordedInode(t *testing.T) {
	base := t.TempDir()
	owner := Owner{"tenant", "app", "gateway", "event", "message"}
	store, err := Open(base, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Create(brokenDownload{}, ""); err == nil {
		t.Fatal("failed download reported success")
	}
	if len(store.manifest.Files) != 1 || store.manifest.Files[0].SHA256 != "" {
		t.Fatal("missing incomplete manifest entry")
	}
	partial := filepath.Join(store.dir, store.manifest.Files[0].Name)
	reopened, err := Open(base, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.manifest.Files) != 0 {
		t.Fatal("incomplete entry retained")
	}
	if _, err = os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("partial download retained")
	}
}

func TestCleanupRetainsChangedAndForeignFiles(t *testing.T) {
	base := t.TempDir()
	owner := Owner{"tenant", "app", "gateway", "event", "message"}
	store, err := Open(base, owner)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.Create(strings.NewReader("original"), "")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := ExpectedHash(base, owner, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("user replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Cleanup(base, owner, path, hash); !errors.Is(err, ErrUntrustedSource) {
		t.Fatalf("replacement accepted: %v", err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "user replacement" {
		t.Fatal("replacement deleted")
	}
	foreign := filepath.Join(t.TempDir(), "user-file")
	if err = os.WriteFile(foreign, []byte("user"), 0600); err != nil {
		t.Fatal(err)
	}
	if IsManagedPath(base, owner, foreign) {
		t.Fatal("foreign path is managed")
	}
}
