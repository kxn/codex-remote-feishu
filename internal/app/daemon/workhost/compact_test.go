package workhost

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestNonCodexCompactIsDurableSafeRebuildAndUnknownStopIsOnlyObserved(t *testing.T) {
	ctx := context.Background()
	launches, stops := 0, 0
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { launches++; return 100 + launches, nil })
	driver := &rebuildDriver{stopped: true}
	h.config.Runtime = driver
	h.config.StopCarrier = func(context.Context, work.WorkerSession) error {
		stops++
		if stops == 1 {
			return errors.New("exit acknowledgement lost")
		}
		return nil
	}
	var authority workstore.PrivateActionSource
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "compact-event", Tenant: "tenant", ChatID: "private", ChatType: "p2p", MessageID: "compact-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "compact-human"}, MessageType: "text", RawContent: `{"text":"/compact"}`, ReceivedAt: h.config.Now()}
		ingress, _, err := tx.AcceptRawIngress(raw, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
		if err != nil {
			return err
		}
		principal, _, err := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: ingress.ID})
		if err != nil {
			return err
		}
		if err = tx.PutScope(work.WorkScope{ID: "human-scope", Kind: work.ScopePersonal, PrincipalID: principal.ID, RoleID: "role", Revision: 1}, 0); err != nil {
			return err
		}
		if err = tx.PutContext(work.WorkContext{ID: "human-context", ScopeID: "human-scope", RoleID: "role", Workstream: "main", Checkpoint: "preserve scoped checkpoint", Revision: 1}, 0); err != nil {
			return err
		}
		if err = tx.PutPrivateCommand(work.TypedCommandEnvelope{ID: "compact-source", IngressID: ingress.ID, ScopeID: "human-scope", ContextID: "human-context", PrincipalID: principal.ID, RoleID: "role", GatewayRef: "gateway", Text: "/compact", State: "sending", Revision: 1}, 0); err != nil {
			return err
		}
		authority = workstore.PrivateActionSource{PersonalScopeID: "human-scope", CommandID: "compact-source"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	need := workengine.WorkerNeed{ScopeID: "human-scope", RoleID: "role", ContextID: "human-context"}
	if err := h.Prepare(ctx, need); err != nil {
		t.Fatal(err)
	}
	var before work.WorkerSession
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		workers, e := tx.ListWorkers("human-scope")
		if e != nil {
			return e
		}
		before = workers[0]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	opID, err := h.RequestCompact(ctx, before.ID, "human-request", authority)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.RunCompactions(ctx, 16); err != nil {
		t.Fatal(err)
	}
	if stops != 0 || launches != 1 {
		t.Fatal("compact touched a carrier before it became ready")
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		if tx.AssertHostCompactionAvailable(before.ID, before.Generation) == nil {
			t.Fatal("queued rebuild did not close the next admission")
		}
		w, e := tx.GetWorker(before.ID)
		if e != nil {
			return e
		}
		revision := w.Revision
		w.State = work.WorkerReady
		w.Revision++
		return tx.PutWorker(w, revision)
	}); err != nil {
		t.Fatal(err)
	}
	if err = h.RunCompactions(ctx, 16); err == nil || launches != 1 {
		t.Fatal("unknown stop blindly launched a replacement")
	}
	if err = h.RunCompactions(ctx, 16); err != nil {
		t.Fatal(err)
	}
	if launches != 2 || stops != 2 || driver.inspections != 1 {
		t.Fatalf("replayed native effects or re-inspected dead carrier: launch=%d stopObservations=%d inspect=%d", launches, stops, driver.inspections)
	}
	if err = h.RunCompactions(ctx, 16); err != nil {
		t.Fatal(err)
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(opID)
		if e != nil {
			return e
		}
		if op.State != work.OperationConfirmed || !strings.Contains(op.ExternalReceipt, "verified_fresh_history_rebuild") {
			t.Fatal("safe rebuild was mislabeled native compression")
		}
		w, e := tx.GetWorker(before.ID)
		if e != nil {
			return e
		}
		if w.Generation != before.Generation+1 || w.CarrierIncarnation == before.CarrierIncarnation || w.NativeSessionID != "" {
			t.Fatal("compact reused the old native process/history")
		}
		wc, e := tx.GetContext(w.ContextID)
		if e != nil {
			return e
		}
		if wc.Checkpoint != "preserve scoped checkpoint" {
			t.Fatal("compact discarded durable checkpoint")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
