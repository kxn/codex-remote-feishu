package daemon

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// This opt-in integration probe copies each authority DB into a private temp
// directory using SQLite's consistent backup. Original journals and history
// are opened read-only; it prints aggregate results only.
func TestLegacyHistoryAllowedOnReadOnlySourceCopies(t *testing.T) {
	root, home := os.Getenv("PRIVATE_ALIAS_PROBE_STATE"), os.Getenv("PRIVATE_ALIAS_PROBE_HOME")
	if root == "" || home == "" {
		t.Skip("opt-in read-only production evidence")
	}
	dst := t.TempDir()
	for _, name := range []string{"team-work.db", "private-chat.db", "application-ingress.db"} {
		in, err := sql.Open("sqlite", "file:"+filepath.Join(root, name)+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		output := strings.ReplaceAll(filepath.Join(dst, name), "'", "''")
		_, err = in.Exec("VACUUM INTO '" + output + "'")
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(dst, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var receiptRaw []byte
	if err = raw.QueryRow("SELECT receipt FROM private_migration_imports LIMIT 1").Scan(&receiptRaw); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	var receipt privatechat.MigrationImportReceipt
	if json.Unmarshal(receiptRaw, &receipt) != nil {
		t.Fatal("bad receipt")
	}
	owner := receipt.Owner
	cfg := config.DefaultAppConfig()
	enabled := true
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: owner.GatewayID, AppID: owner.AppID, AppSecret: "fixture", Enabled: &enabled}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: owner.GatewayID, Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID})
	store, err := workstore.Open(t.Context(), filepath.Join(dst, "team-work.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	journal, err := privatechat.OpenJournal(filepath.Join(dst, "private-chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	coordinator := &personalMigrationCoordinator{app: app, journal: journal, limits: workstore.PersonalExportLimits{MaxRows: 100000, MaxBytes: 512 << 20}}
	router, err := ingressowner.Open(filepath.Join(dst, "application-ingress.db"), nil, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	coordinator.router = router
	router.SetOwnerScopeBindingVerifier(teamMigrationOwnerVerifier{store: store})
	app.teamRuntime = &teamRuntime{store: store, app: app}
	app.applications = &applicationRuntime{journal: journal, migrations: coordinator}
	app.headlessRuntime.Paths.StateDir = root
	app.headlessRuntime.BaseEnv = []string{"HOME=" + home}
	g := &applicationIngress{host: app, router: router}
	source, _, err := g.legacyHistorySource(t.Context(), owner, receipt.Generation)
	if err != nil {
		t.Fatalf("real committed source: %v", err)
	}

	var ids []string
	if err = store.Within(t.Context(), func(tx *workstore.Tx) error { var e error; ids, e = tx.HistoricalSessionIDs(); return e }); err != nil {
		t.Fatal(err)
	}
	visible, ready := 0, 0
	var selectedID, selectedNS, selectedCarrier string
	for _, id := range ids {
		var carriers []workstore.CarrierRegistration
		if err = store.Within(t.Context(), func(tx *workstore.Tx) error { var e error; carriers, e = tx.HistoricalSessionCarriers(id); return e }); err != nil {
			t.Fatal(err)
		}
		if len(carriers) == 0 {
			continue
		}
		launch, e := legacyHistoryLaunch(source, carriers[0])
		if e != nil {
			continue
		}
		app.teamRuntime.codexUpgradeHostID = launch.Snapshot.HostID
		ns, e := nativeHistoryNamespace(agentproto.BackendCodex, app.headlessRuntime.BaseEnv, launch.Snapshot.CWD, launch.Snapshot.HostID)
		if e != nil {
			t.Fatal(e)
		}
		ok, e := g.legacyHistoryAllowed(t.Context(), owner, receipt.Generation, agentproto.BackendCodex, ns, id, false)
		if e != nil {
			t.Fatal(e)
		}
		if ok {
			visible++
			selectedID, selectedNS, selectedCarrier = id, ns, carriers[0].Incarnation
		}
		ok, e = g.legacyHistoryAllowed(t.Context(), owner, receipt.Generation, agentproto.BackendCodex, ns, id, true)
		if e != nil {
			t.Fatal(e)
		}
		if ok {
			ready++
		}
	}
	t.Logf("visible=%d ready=%d", visible, ready)
	if visible != 2 || ready != 1 {
		t.Fatalf("expected two visible, one settled: visible=%d ready=%d", visible, ready)
	}
	// Mutations are limited to the disposable database copies. Introduce a
	// second historical carrier with another owner for the same session.
	mutation, err := sql.Open("sqlite", filepath.Join(dst, "team-work.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = mutation.Exec(`INSERT INTO carrier_registrations(incarnation,worker_id,instance_id,payload,connected) SELECT 'foreign-test-carrier',worker_id,instance_id,json_set(payload,'$.incarnation','foreign-test-carrier','$.owner.scopeId','foreign-scope'),connected FROM carrier_registrations WHERE incarnation=?`, selectedCarrier)
	if err != nil {
		t.Fatal(err)
	}
	result, err := mutation.Exec(`INSERT INTO native_turns(carrier_incarnation,session_id,turn_id,attempt_id,payload) SELECT 'foreign-test-carrier',session_id,turn_id,attempt_id,payload FROM native_turns WHERE carrier_incarnation=? AND session_id=? LIMIT 1`, selectedCarrier, selectedID)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := result.RowsAffected()
	mutation.Close()
	if n != 1 {
		t.Fatal("missing fixture turn for conflict")
	}
	ok, err := g.legacyHistoryAllowed(t.Context(), owner, receipt.Generation, agentproto.BackendCodex, selectedNS, selectedID, false)
	if err != nil || ok {
		t.Fatalf("foreign source was exposed: %v", err)
	}
	target, err := sql.Open("sqlite", filepath.Join(dst, "private-chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = target.Exec(`UPDATE private_migration_imports SET receipt=json_set(receipt,'$.ProofHash','incorrect-proof') WHERE import_id=?`, receipt.ImportID)
	target.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = g.legacyHistorySource(t.Context(), owner, receipt.Generation); err == nil {
		t.Fatal("wrong target proof accepted")
	}
}
