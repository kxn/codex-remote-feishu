package workupgrade

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func upgradeFixture(t *testing.T) (*Coordinator, Request, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("native fake npm shell fixture; Windows cross compiled separately")
	}
	root := t.TempDir()
	store, err := workstore.Open(context.Background(), filepath.Join(root, "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	now := time.Now().UTC()
	source := workstore.PrivateActionSource{PersonalScopeID: "personal", CommandID: "upgrade-human"}
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "description", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "upgrade-event", Tenant: "tenant", ChatID: "private", ChatType: "p2p", MessageID: "upgrade-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"/upgrade codex"}`, ReceivedAt: now}
		ingress, _, e := tx.AcceptRawIngress(raw, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
		if e != nil {
			return e
		}
		principal, _, e := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: ingress.ID})
		if e != nil {
			return e
		}
		if e = tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, PrincipalID: principal.ID, RoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		if e = tx.PutContext(work.WorkContext{ID: "context", ScopeID: "personal", RoleID: "role", Workstream: "main", Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutPrivateCommand(work.TypedCommandEnvelope{ID: "upgrade-human", IngressID: ingress.ID, ScopeID: "personal", ContextID: "context", PrincipalID: principal.ID, RoleID: "role", GatewayRef: "gateway", Text: "/upgrade codex", State: "sending", Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	triple := "x86_64-unknown-linux-musl"
	if runtime.GOARCH == "arm64" {
		triple = "aarch64-unknown-linux-musl"
	}
	if runtime.GOOS == "darwin" {
		triple = strings.ReplaceAll(triple, "unknown-linux-musl", "apple-darwin")
	}
	marker := filepath.Join(root, "global-version")
	log := filepath.Join(root, "installs")
	npm := filepath.Join(root, "npm")
	script := fmt.Sprintf("#!/bin/sh\nset -eu\nprintf 'install\\n' >> %q\nfor item in \"$@\"; do target=${item##*@}; done\nif [ \"$2\" = '--prefix' ]; then\n binary=\"$3/node_modules/@openai/codex/vendor/%s/bin/codex\"\n mkdir -p \"$(dirname \"$binary\")\"\n printf '#!/bin/sh\\nprintf \\\"codex-cli %%s\\\\n\\\"\\n' \"$target\" > \"$binary\"\n chmod 755 \"$binary\"\nelse\n printf '%%s' \"$target\" > %q\nfi\n", log, triple, marker)
	if err = os.WriteFile(npm, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	inspect := func(context.Context, codexupgrade.InspectOptions) codexupgrade.Installation {
		version := "0.1.0"
		if raw, e := os.ReadFile(marker); e == nil {
			version = string(raw)
		}
		return codexupgrade.Installation{ConfiguredBinary: "codex", EffectiveBinary: "codex", ResolvedBinary: "codex", SourceKind: codexupgrade.SourceStandaloneNPM, PackageVersion: version, NPMCommand: npm, PackageRoot: filepath.Join(root, "global-package")}
	}
	coordinator, err := New(store, Config{HostID: "host", StateDir: root, ConfiguredBinary: "codex", Inspect: inspect, Drain: func(context.Context, work.WorkerSession) error { return fmt.Errorf("unexpected carrier") }, Restart: func(context.Context, string, string) error { return fmt.Errorf("unexpected carrier") }})
	if err != nil {
		t.Fatal(err)
	}
	req := Request{ID: "upgrade-candidate", TargetVersion: "0.2.0", InstallationFingerprint: InstallationFingerprint(inspect(context.Background(), codexupgrade.InspectOptions{})), Source: source}
	return coordinator, req, log
}
func TestManagedUpgradeTracksBothInstallersPinsVersionAndReopensAdmission(t *testing.T) {
	c, req, log := upgradeFixture(t)
	ctx := context.Background()
	if _, err := c.Request(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		if tx.AssertNativeUpgradeAvailable(work.BackendCodex, "host") == nil {
			t.Fatal("Codex admission was not fenced")
		}
		return tx.AssertNativeUpgradeAvailable(work.BackendClaude, "host")
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(ctx, 1); err != nil {
		t.Fatal(err)
	}
	result, err := c.Status(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stage != "completed" || result.ManagedVersion != "0.2.0" || result.ManagedBinary == "" || result.Global.Result == nil || result.Private.Result == nil {
		t.Fatalf("upgrade did not finish verified phases: %+v", result)
	}
	if _, err = c.Request(ctx, req); err != nil {
		t.Fatalf("postinstall same candidate lost idempotence: %v", err)
	}
	req.TargetVersion = "0.3.0"
	if _, err = c.Request(ctx, req); err == nil {
		t.Fatal("same candidate changed target")
	}
	raw, _ := os.ReadFile(log)
	if strings.Count(string(raw), "install") != 2 {
		t.Fatalf("installer rerun: %s", raw)
	}
	if err = c.store.Within(ctx, func(tx *workstore.Tx) error { return tx.AssertNativeUpgradeAvailable(work.BackendCodex, "host") }); err != nil {
		t.Fatal(err)
	}
}
func TestManagedUpgradeLostExitReceiptIsObservedWithoutRepeatingInstall(t *testing.T) {
	c, req, log := upgradeFixture(t)
	ctx := context.Background()
	first := true
	c.config.Install = func(ctx context.Context, version string, opts codexupgrade.TrackedInstallOptions) (codexupgrade.TrackedInstallResult, error) {
		if first {
			first = false
			opts.OnExited = func(codexupgrade.TrackedInstallResult) error {
				return fmt.Errorf("simulated daemon death before exit receipt")
			}
		}
		return codexupgrade.InstallTracked(ctx, version, opts)
	}
	if _, err := c.Request(ctx, req); err != nil {
		t.Fatal(err)
	}
	if c.Run(ctx, 1) == nil {
		t.Fatal("lost receipt should remain uncertain")
	}
	before, err := c.Status(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Stage != "unknown" || before.Global.Scope == nil {
		t.Fatalf("uncertainty lost: %+v", before)
	}
	reopened, err := New(c.store, c.config)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.Run(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err = reopened.Run(ctx, 1); err != nil {
		t.Fatal(err)
	}
	after, err := reopened.Status(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Stage != "completed" || !after.Global.RecoveredVerified {
		t.Fatalf("exact installer observation did not recover: %+v", after)
	}
	raw, _ := os.ReadFile(log)
	if strings.Count(string(raw), "install") != 2 {
		t.Fatalf("unknown installer blindly repeated: %s", raw)
	}
}

func TestUpgradeRejectsUnattributedLegacyAndKeepsUnknownOnLateReconnect(t *testing.T) {
	c, req, log := upgradeFixture(t)
	ctx := context.Background()
	if err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.PutMigrationResourceFence(workstore.MigrationResourceFence{ResourceKey: "native-installation:codex:unverified-host", MigrationID: "migration", RecordKey: "legacy-native", Reason: "legacy_unknown", Evidence: []byte(`{"native":{"backend":"codex"}}`)})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Request(ctx, req); err == nil {
		t.Fatal("unattributed legacy execution could share global npm installation")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("legacy fence allowed install")
	}
	c, req, log = upgradeFixture(t)
	if _, err := c.Request(ctx, req); err != nil {
		t.Fatal(err)
	}
	original := c.config.Install
	c.config.Install = func(ctx context.Context, version string, opts codexupgrade.TrackedInstallOptions) (codexupgrade.TrackedInstallResult, error) {
		result, err := original(ctx, version, opts)
		if err != nil {
			return result, err
		}
		if opts.Prefix == "" {
			err = c.store.Within(ctx, func(tx *workstore.Tx) error {
				if err := tx.PutMigrationResourceFence(workstore.MigrationResourceFence{ResourceKey: "native-installation:codex:unverified-host", MigrationID: "migration", RecordKey: "late-legacy", Reason: "legacy_live", Evidence: []byte(`{"native":{"backend":"codex"}}`)}); err != nil {
					return err
				}
				return tx.ObserveNativeUpgradeLegacyConflict(work.BackendCodex, "")
			})
		}
		return result, err
	}
	if c.Run(ctx, 1) == nil {
		t.Fatal("legacy reconnect during installer must interrupt saga")
	}
	record, err := c.Status(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Stage != "unknown" || record.Private.Started {
		t.Fatalf("late unknown ignored: %+v", record)
	}
	if c.Run(ctx, 1) == nil {
		t.Fatal("unresolved legacy execution unexpectedly resumed upgrade")
	}
	raw, _ := os.ReadFile(log)
	if strings.Count(string(raw), "install") != 1 {
		t.Fatalf("legacy conflict repeated/continued installation: %s", raw)
	}
}
