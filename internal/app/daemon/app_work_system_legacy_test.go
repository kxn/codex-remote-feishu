package daemon

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess"
)

func seedLegacyAdminOperation(t *testing.T, a *App, source teamHumanSource, text, kind string, state work.OperationState, receipt string) string {
	t.Helper()
	id := teamID("human-host", source.Key+":"+kind)
	pickerTx(t, a, func(tx *workstore.Tx) error {
		raw, err := json.Marshal(struct {
			Text   string
			Source workstore.PrivateActionSource
		}{text, source.Authority})
		if err != nil {
			return err
		}
		body := work.NewContent(id+":request", source.ScopeID, "application/json", raw, time.Now().UTC())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{ID: id, ScopeID: source.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, Kind: kind, TargetID: "installation", TargetGeneration: 1, IdempotencyKey: source.Key, RequestRef: body.ID, RequestHash: body.SHA256, State: state, ExternalReceipt: receipt, Revision: 1}, 0)
		return err
	})
	return id
}

func TestSystemLegacyConfirmedPlainAdminReceiptKeepsHashAndDoesNotRepeatWrite(t *testing.T) {
	a, source, home := systemMenuFixture(t)
	path := filepath.Join(home, "config.json")
	loaded, err := config.LoadAppConfigAtPath(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Config.ExternalAccess.NetworkMode = "wan"
	if err = config.WriteAppConfig(path, loaded.Config); err != nil {
		t.Fatal(err)
	}
	a.externalAccessRuntime.Settings.NetworkMode = "wan"
	seedLegacyAdminOperation(t, a, source, "/admin network local", "host.admin.network_set", work.OperationConfirmed, "legacy actual saved receipt: local")
	id, err := a.handleTeamAdminCommand(t.Context(), source, control.Action{Text: "/admin network local"})
	if err != nil {
		t.Fatal(err)
	}
	if card := systemCard(t, a, source.ScopeID, id); !strings.Contains(card, "legacy actual saved receipt: local") {
		t.Fatalf("legacy plain receipt was lost or request hash drifted: %s", card)
	}
	loaded, err = config.LoadAppConfigAtPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.ExternalAccess.NetworkMode != "wan" {
		t.Fatal("legacy confirmed admin mutation executed again")
	}
}

func TestSystemLegacyWebRequestNeverCreatesReplacementAsyncIssuance(t *testing.T) {
	for _, state := range []work.OperationState{work.OperationConfirmed, work.OperationSending, work.OperationUnknown} {
		t.Run(string(state), func(t *testing.T) {
			a, source, _ := systemMenuFixture(t)
			provider := &systemWebProvider{}
			a.externalAccessRuntime.Settings.NetworkMode = "wan"
			a.externalAccess = externalaccess.NewService(externalaccess.Options{Provider: provider})
			receipt := ""
			if state == work.OperationConfirmed {
				receipt = "临时管理页：https://old-request.fixture.invalid/existing-token\n有效期至 old-expiry"
			}
			legacy := seedLegacyAdminOperation(t, a, source, "/admin web", "host.admin.web", state, receipt)
			id, err := a.handleTeamAdminCommand(t.Context(), source, control.Action{Text: "/admin web"})
			if err != nil {
				t.Fatal(err)
			}
			if err = a.runTeamSystemHostJobs(t.Context()); err != nil {
				t.Fatal(err)
			}
			if provider.calls.Load() != 0 {
				t.Fatal("legacy web replay signed another URL")
			}
			card := systemCard(t, a, source.ScopeID, id)
			if state == work.OperationConfirmed && !strings.Contains(card, "existing-token") {
				t.Fatal("legacy issued URL receipt was not reused")
			}
			if state != work.OperationConfirmed && !strings.Contains(card, "不会重复执行") {
				t.Fatalf("legacy unknown issuance was not kept uncertain: %s", card)
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				if _, err := tx.GetOperation(teamID("system-web", source.Key)); err != workstore.ErrNotFound {
					t.Fatal("legacy operation was bypassed by a new async job")
				}
				op, err := tx.GetOperation(legacy)
				if op.State != state {
					t.Fatal("legacy operation state was rewritten during replay")
				}
				return err
			})
		})
	}
}
