package relayruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishLockHidesIncompleteRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.lock")
	err := publishLock(path, func(writer io.Writer) error {
		if _, err := io.WriteString(writer, `{"pid":`); err != nil {
			return err
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("lock is visible before its record is complete: %v", err)
		}
		_, err := io.WriteString(writer, `123,"token":"complete","createdAt":"2026-11-13T00:00:00Z"}`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record lockRecord
	if err := json.Unmarshal(raw, &record); err != nil || record.PID != 123 || record.Token != "complete" {
		t.Fatalf("published incomplete record: %q, %v", raw, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "relay.lock" {
		t.Fatalf("unexpected files after publication: %v, %v", entries, err)
	}
}

func TestPublishLockPreservesExistingOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.lock")
	if err := os.WriteFile(path, []byte(`{"pid":123,"token":"owner"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := publishLock(path, func(writer io.Writer) error {
		_, err := io.WriteString(writer, `{"pid":456,"token":"contender"}`)
		return err
	})
	if !os.IsExist(err) {
		t.Fatalf("expected existing owner to reject publication: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != `{"pid":123,"token":"owner"}` {
		t.Fatalf("replaced existing owner: %q, %v", raw, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain after rejected publication: %v, %v", entries, err)
	}
}

func TestPublishLockCleansUpFailedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.lock")
	writeErr := errors.New("record write failed")
	err := publishLock(path, func(writer io.Writer) error {
		if _, err := io.WriteString(writer, `{"pid":`); err != nil {
			return err
		}
		return writeErr
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected original write error, got %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed record left files behind: %v, %v", entries, err)
	}
}

func TestAcquireLockWaitingHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.lock")
	owner, err := AcquireLock(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	contender, err := AcquireLock(ctx, path, true)
	if !errors.Is(err, context.Canceled) || contender != nil {
		t.Fatalf("waiting contender ignored cancellation: %v, %v", contender, err)
	}
}

func TestFileLockReleasePreservesDifferentToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.lock")
	lock, err := AcquireLock(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"pid":123,"token":"replacement"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("release removed a different owner's lock: %v", err)
	}
}
