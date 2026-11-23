package daemon

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
)

func TestPrivateUpgradeDrainRejectsUnfrozenCarrier(t *testing.T) {
	host := &privateNativeHost{store: &privateNativeHostStore{}}
	registry, err := executionowner.Open(filepath.Join(t.TempDir(), "registry.db"), host)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	host.registry = registry
	host.outbound, err = executionowner.NewOutbound(registry)
	if err != nil {
		t.Fatal(err)
	}
	m, err := registry.BeginMaintenance(t.Context(), "codex", "upgrade", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	reg := executionowner.Registration{Carrier: executionowner.Carrier{InstanceID: "instance", Incarnation: "unfrozen", Backend: "codex", Namespace: "account", Generation: 1}, Owner: executionowner.Owner{Application: executionowner.Private, ID: "owner", Epoch: 1}}
	if err = host.drainPrivateUpgradeCarrier(t.Context(), m, reg); !errors.Is(err, executionowner.ErrConflict) {
		t.Fatalf("unfrozen carrier: %v", err)
	}
}
