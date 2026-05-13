package desktopsession

import (
	"path/filepath"
	"testing"
)

func TestStatusFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop-session.json")
	want := Status{
		State:         StateBackendOnly,
		BackendPID:    1234,
		InstanceID:    "stable",
		AdminURL:      "http://localhost:9501/admin/",
		SetupURL:      "http://localhost:9501/setup",
		SetupRequired: true,
	}
	if err := WriteStatusFile(path, want); err != nil {
		t.Fatalf("WriteStatusFile: %v", err)
	}

	got, ok, err := ReadStatusFile(path)
	if err != nil {
		t.Fatalf("ReadStatusFile: %v", err)
	}
	if !ok {
		t.Fatal("expected status file to exist")
	}
	if got.State != want.State || got.BackendPID != want.BackendPID || got.InstanceID != want.InstanceID {
		t.Fatalf("status identity = %#v, want %#v", got, want)
	}
	if got.AdminURL != want.AdminURL || got.SetupURL != want.SetupURL || got.SetupRequired != want.SetupRequired {
		t.Fatalf("status urls = %#v, want %#v", got, want)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be populated")
	}

	if err := RemoveStatusFile(path); err != nil {
		t.Fatalf("RemoveStatusFile: %v", err)
	}
	if _, ok, err := ReadStatusFile(path); err != nil {
		t.Fatalf("ReadStatusFile after remove: %v", err)
	} else if ok {
		t.Fatal("expected removed status file to be absent")
	}
}

func TestStatusFileHelpersIgnoreEmptyPath(t *testing.T) {
	if _, ok, err := ReadStatusFile(""); err != nil {
		t.Fatalf("ReadStatusFile(empty): %v", err)
	} else if ok {
		t.Fatal("expected empty path to report absent")
	}
	if err := WriteStatusFile("", Status{State: StateBackendOnly}); err != nil {
		t.Fatalf("WriteStatusFile(empty): %v", err)
	}
	if err := RemoveStatusFile(""); err != nil {
		t.Fatalf("RemoveStatusFile(empty): %v", err)
	}
}
