package executionowner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type maintenanceVerifier struct{ allow bool }

func (v maintenanceVerifier) WithMaintenanceUnspawned(_ context.Context, _ Maintenance, _ Registration, _ Registration, _ string, commit func() error) error {
	if !v.allow {
		return ErrConflict
	}
	return commit()
}

func TestMaintenanceUnspawnedRequiresFrozenProofAndNoOldRegistration(t *testing.T) {
	ctx := context.Background()
	r, err := Open(filepath.Join(t.TempDir(), "registry.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	m, err := r.BeginMaintenance(ctx, "codex", "unspawned", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceQuiescent(ctx, m, "sources", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalled(ctx, m, "install-proof", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	old := Registration{Carrier: Carrier{InstanceID: "old", Incarnation: "old-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Team, "worker", 1}}
	next := Registration{Carrier: Carrier{InstanceID: "new", Incarnation: "new-inc", Backend: "codex", Namespace: "account", Generation: 2}, Owner: Owner{Team, "worker", 2}}
	if err = r.RegisterMaintenanceUnspawned(ctx, m, old, next, "unsent-proof", maintenanceVerifier{}); err == nil {
		t.Fatal("untrusted unsent proof accepted")
	}
	if err = r.RegisterMaintenanceUnspawned(ctx, m, old, next, "unsent-proof", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.RegisterMaintenanceUnspawned(ctx, m, old, next, "unsent-proof", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	foreign := next
	foreign.Carrier.Incarnation = "foreign"
	if err = r.RegisterMaintenanceUnspawned(ctx, m, old, foreign, "unsent-proof", maintenanceVerifier{true}); err == nil {
		t.Fatal("another replacement accepted")
	}
	if err = r.MarkMaintenanceRecovered(ctx, m, old, &next, "hello-proof", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceUnspawnedSessionRequiresExactStoppedOwner(t *testing.T) {
	ctx := context.Background()
	r, err := Open(filepath.Join(t.TempDir(), "registry.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	prior := Registration{Carrier: Carrier{InstanceID: "prior", Incarnation: "prior-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Team, "worker", 1}}
	if err = r.Register(ctx, prior); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, prior, "session"); err != nil {
		t.Fatal(err)
	}
	if err = r.Retire(ctx, prior, "proof"); err != nil {
		t.Fatal(err)
	}
	m, err := r.BeginMaintenance(ctx, "codex", "upgrade-session", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceQuiescent(ctx, m, "sources", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalled(ctx, m, "installed", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	old := Registration{Carrier: Carrier{InstanceID: "old", Incarnation: "old-inc", Backend: "codex", Namespace: "account", Generation: 2}, Owner: Owner{Team, "worker", 1}}
	next := Registration{Carrier: Carrier{InstanceID: "new", Incarnation: "new-inc", Backend: "codex", Namespace: "account", Generation: 3}, Owner: Owner{Team, "worker", 2}}
	if err = r.RegisterMaintenanceUnspawned(ctx, m, old, next, "proof-unsent", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.ReserveMaintenanceUnspawnedSession(ctx, m, old, next, "foreign", "proof-unsent", maintenanceVerifier{true}); err == nil {
		t.Fatal("unknown session accepted")
	}
	if err = r.ReserveMaintenanceUnspawnedSession(ctx, m, old, next, "session", "proof-unsent", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, next, "session"); err != nil {
		t.Fatal(err)
	}
	if err = r.ReserveMaintenanceUnspawnedSession(ctx, m, old, next, "session", "proof-unsent", maintenanceVerifier{true}); err != nil {
		t.Fatalf("replay after native bind: %v", err)
	}
}

func (v maintenanceVerifier) WithMaintenanceInstallUnsent(_ context.Context, _ Maintenance, _ string, commit func() error) error {
	if !v.allow {
		return ErrConflict
	}
	return commit()
}

func TestMaintenanceTeamControllerEpochRecovery(t *testing.T) {
	ctx := context.Background()
	r, err := Open(filepath.Join(t.TempDir(), "registry.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	old := Registration{Carrier: Carrier{InstanceID: "old", Incarnation: "old-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Team, "worker", 3}}
	if err = r.Register(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, old, "session"); err != nil {
		t.Fatal(err)
	}
	m, err := r.BeginMaintenance(ctx, "codex", "team-upgrade", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Retire(ctx, old, "proof"); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceQuiescent(ctx, m, "sources", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch); err != nil {
		t.Fatal(err)
	}
	if err = r.RecoverMaintenanceInstallUnsent(ctx, m, "unsent-proof", maintenanceVerifier{}); err == nil {
		t.Fatal("untrusted proof reset an installing operation")
	}
	if err = r.RecoverMaintenanceInstallUnsent(ctx, m, "unsent-proof", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalled(ctx, m, "install-proof", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	next := Registration{Carrier: Carrier{InstanceID: "new", Incarnation: "new-inc", Backend: "codex", Namespace: "account", Generation: 2}, Owner: Owner{Team, "worker", 4}}
	bad := next
	bad.Owner.Epoch++
	if err = r.RegisterMaintenanceRecovery(ctx, m, old, bad); err == nil {
		t.Fatal("accepted skipped controller epoch")
	}
	if err = r.RegisterMaintenanceRecovery(ctx, m, old, next); err != nil {
		t.Fatal(err)
	}
	recoveries, err := r.MaintenanceRecoveries(ctx, m, "", 64)
	if err != nil || len(recoveries) != 1 || recoveries[0].Old != old || recoveries[0].Replacement == nil || *recoveries[0].Replacement != next {
		t.Fatalf("frozen replacement inventory: %#v %v", recoveries, err)
	}
	if err = r.ReserveMaintenanceSession(ctx, m, old, next, "session"); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, next, "session"); err != nil {
		t.Fatal(err)
	}
}

func (v maintenanceVerifier) VerifyMaintenanceSources(context.Context, Maintenance, string) error {
	if !v.allow {
		return ErrConflict
	}
	return nil
}
func (v maintenanceVerifier) VerifyMaintenanceInstall(context.Context, Maintenance, string) error {
	if !v.allow {
		return ErrConflict
	}
	return nil
}
func (v maintenanceVerifier) WithMaintenanceRelease(_ context.Context, _ Maintenance, _ string, commit func() error) error {
	if !v.allow {
		return ErrConflict
	}
	return commit()
}
func (v maintenanceVerifier) VerifyMaintenanceRestart(context.Context, Maintenance, string) error {
	if !v.allow {
		return ErrConflict
	}
	return nil
}
func (v maintenanceVerifier) WithMaintenanceRecovery(_ context.Context, _ Maintenance, _ Registration, _ *Registration, _ string, commit func() error) error {
	if !v.allow {
		return ErrConflict
	}
	return commit()
}
func TestMaintenanceFencesBusinessAndRequiresProofBeforeRelease(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "registry.db")
	r, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	old := Registration{Carrier: Carrier{InstanceID: "old", Incarnation: "old-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Private, "owner", 1}}
	if err = r.Register(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, old, "session"); err != nil {
		t.Fatal(err)
	}
	m, err := r.BeginMaintenance(ctx, "codex", "maintenance", "v2")
	if err != nil {
		t.Fatal(err)
	}
	other := Registration{Carrier: Carrier{InstanceID: "other", Incarnation: "other-inc", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Team, "team", 1}}
	if err = r.Register(ctx, other); !errors.Is(err, ErrMaintenanceActive) {
		t.Fatalf("new carrier bypassed fence: %v", err)
	}
	if err = r.MarkMaintenanceQuiescent(ctx, m, "source", maintenanceVerifier{true}); !errors.Is(err, ErrMaintenanceNotQuiescent) {
		t.Fatalf("live carrier treated stopped: %v", err)
	}
	if err = r.Retire(ctx, old, "proof"); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceQuiescent(ctx, m, "source", maintenanceVerifier{}); err == nil {
		t.Fatal("untrusted legacy-source fence accepted")
	}
	if err = r.MarkMaintenanceQuiescent(ctx, m, "source", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	status, err := r.MaintenanceStatus(ctx, "codex")
	if err != nil || status.Phase != "installing" {
		t.Fatalf("open guessed restart: %#v %v", status, err)
	}
	if err = r.MarkMaintenanceInstallUnknown(ctx, m, "restart", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch); err == nil {
		t.Fatal("unknown install was retried")
	}
	if err = r.MarkMaintenanceInstalled(ctx, m, "version-proof", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceInstalled(ctx, m, "version-proof", maintenanceVerifier{true}); err != nil {
		t.Fatalf("replay installed evidence: %v", err)
	}
	if err = r.MarkMaintenanceInstalled(ctx, m, "different-proof", maintenanceVerifier{true}); err == nil {
		t.Fatal("accepted different installed evidence on replay")
	}
	if err = r.ReleaseMaintenance(ctx, m, "release-proof", maintenanceVerifier{true}); !errors.Is(err, ErrMaintenanceNotQuiescent) {
		t.Fatalf("released before recovery: %v", err)
	}
	next := Registration{Carrier: Carrier{InstanceID: "new", Incarnation: "new-inc", Backend: "codex", Namespace: "account", Generation: 2}, Owner: old.Owner}
	if err = r.RegisterMaintenanceRecovery(ctx, m, old, next); err != nil {
		t.Fatal(err)
	}
	if err = r.ReserveMaintenanceSession(ctx, m, old, next, "foreign-session"); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign recovery session: %v", err)
	}
	if err = r.ReserveMaintenanceSession(ctx, m, old, next, "session"); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, next, "session"); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceRecovered(ctx, m, old, &next, "hello-proof", maintenanceVerifier{}); err == nil {
		t.Fatal("untrusted recovery accepted")
	}
	if err = r.MarkMaintenanceRecovered(ctx, m, old, &next, "hello-proof", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkMaintenanceRecovered(ctx, m, old, &next, "hello-proof", maintenanceVerifier{true}); err != nil {
		t.Fatalf("replay recovery evidence: %v", err)
	}
	if err = r.MarkMaintenanceRecovered(ctx, m, old, &next, "other-proof", maintenanceVerifier{true}); err == nil {
		t.Fatal("accepted different recovery evidence on replay")
	}
	if err = r.ReleaseMaintenance(ctx, m, "release-proof", maintenanceVerifier{true}); err != nil {
		t.Fatal(err)
	}
	if err = r.ReleaseMaintenance(ctx, m, "release-proof", maintenanceVerifier{true}); err != nil {
		t.Fatalf("replay release evidence: %v", err)
	}
	if err = r.ReleaseMaintenance(ctx, m, "other-proof", maintenanceVerifier{true}); err == nil {
		t.Fatal("accepted different release evidence on replay")
	}
	if err = r.Register(ctx, other); err != nil {
		t.Fatalf("admission did not reopen: %v", err)
	}
}
