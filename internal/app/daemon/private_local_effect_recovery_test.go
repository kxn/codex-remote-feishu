package daemon

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPrivateLocalEffectReceiptFailureRecoversInSameProcess(t *testing.T) {
	for _, kind := range []string{"file", "configuration"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dbPath := filepath.Join(root, "private.db")
			owner := privatechat.Owner{TenantID: "tenant", AppID: "app", GatewayID: "g", SurfaceID: "s", ChatID: "chat", UserID: "user"}
			seed := privatechat.New(nil, orchestrator.Config{})
			if err := seed.RegisterOwner(owner); err != nil {
				t.Fatal(err)
			}
			core, err := seed.Service().ExportPrivateCheckpoint(owner.SurfaceID)
			if err != nil {
				t.Fatal(err)
			}
			command := control.DaemonCommand{Kind: control.DaemonCommandAdmin, SurfaceSessionID: owner.SurfaceID, GatewayID: owner.GatewayID, Text: "/admin network"}
			if kind == "file" {
				file := filepath.Join(root, "report.txt")
				if err = os.WriteFile(file, []byte("receipt"), 0600); err != nil {
					t.Fatal(err)
				}
				command.Kind = control.DaemonCommandPrivateSendFile
				command.LocalPath = file
			}
			body, err := privatechat.EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindDaemonCommand, SurfaceSessionID: owner.SurfaceID, GatewayID: owner.GatewayID, DaemonCommand: &command})
			if err != nil {
				t.Fatal(err)
			}
			j, err := privatechat.OpenJournal(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			app := privatechat.New(nil, orchestrator.Config{})
			gateway := &privateFileTestGateway{}
			host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
			host.service = app.Service()
			host.SetExternalAccess(ExternalAccessRuntimeConfig{Settings: externalAccessSettingsView{NetworkMode: "local"}})
			p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
			if err != nil {
				t.Fatal(err)
			}
			if err = j.CommitCheckpoint(t.Context(), privatechat.Checkpoint{Owner: owner, Generation: 1, Revision: 1, Core: core}, 0, []privatechat.Effect{{ID: "effect", Lane: "background", Body: body}}); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err = db.Exec(`CREATE TRIGGER fail_local_receipt BEFORE INSERT ON private_local_receipts BEGIN SELECT RAISE(FAIL,'injected receipt failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`CREATE TRIGGER fail_unknown BEFORE UPDATE OF state ON private_effect WHEN NEW.state='unknown' BEGIN SELECT RAISE(FAIL,'injected transition failure'); END`); err != nil {
				t.Fatal(err)
			}
			r := &privateWorkRunner{host: host, processor: p, journal: j}
			record, err := j.LoadEffect(t.Context(), owner, 1, "effect")
			if err != nil {
				t.Fatal(err)
			}
			work := privatechat.PendingEffectWork{Owner: owner, Generation: 1, Effect: record}
			r.scheduleEffect(t.Context(), work)
			r.effectWG.Wait()
			record, err = j.LoadEffect(t.Context(), owner, 1, "effect")
			if err != nil || record.State != "sending" {
				t.Fatalf("state %s: %v", record.State, err)
			}
			if kind == "file" && len(gateway.files) != 1 {
				t.Fatalf("first upload count=%d", len(gateway.files))
			}
			if _, err = db.Exec(`DROP TRIGGER fail_unknown`); err != nil {
				t.Fatal(err)
			}
			work.Effect = record
			r.scheduleEffect(t.Context(), work)
			r.effectWG.Wait()
			if kind == "file" && len(gateway.files) != 1 {
				t.Fatal("host call repeated during recovery")
			}
			record, err = j.LoadEffect(t.Context(), owner, 1, "effect")
			if err != nil || record.State != "unknown" {
				t.Fatalf("retry transition %s: %v", record.State, err)
			}
			if _, err = db.Exec(`DROP TRIGGER fail_local_receipt`); err != nil {
				t.Fatal(err)
			}
			host.SetExternalAccess(ExternalAccessRuntimeConfig{Settings: externalAccessSettingsView{NetworkMode: "wan"}})
			work.Effect = record
			r.scheduleEffect(t.Context(), work)
			r.effectWG.Wait()
			record, err = j.LoadEffect(t.Context(), owner, 1, "effect")
			if err != nil || record.State != "confirmed" {
				t.Fatalf("recovered state %s: %v", record.State, err)
			}
			if kind == "file" {
				if len(gateway.files) != 1 {
					t.Fatal("unknown file upload was resent")
				}
				var receipt privateFileReceipt
				if json.Unmarshal(record.Receipt, &receipt) != nil || !receipt.Unknown {
					t.Fatalf("not unknown: %s", record.Receipt)
				}
			} else {
				var receipt privateSystemReceipt
				if err = json.Unmarshal(record.Receipt, &receipt); err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(receipt.Events)
				if !strings.Contains(string(encoded), "wan") {
					t.Fatalf("recovery did not read current configuration: %s", encoded)
				}
			}
		})
	}
}

func TestReturnedLocalRecoveryDoesNotChangeAsyncOrAlreadySending(t *testing.T) {
	for _, kind := range []control.DaemonCommandKind{control.DaemonCommandUpgrade, control.DaemonCommandUpgradeOwnerFlow} {
		if !localEffectReturnsAfterHostCall(kind) {
			t.Fatalf("upgrade %s must recover its returned call", kind)
		}
	}
	for _, kind := range []control.DaemonCommandKind{control.DaemonCommandStartHeadless, control.DaemonCommandKillHeadless, control.DaemonCommandPrivateProjectShare} {
		if localEffectReturnsAfterHostCall(kind) {
			t.Fatalf("asynchronous effect %s is not local", kind)
		}
	}
}
