package worktransport

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type reviewTerminalRegistrar struct{ f *nativeResponseFixture }

func (r reviewTerminalRegistrar) RegisterCommand(ctx context.Context, carrier string, command agentproto.Command) error {
	if e := r.f.coordinator.HandleEvent(ctx, "instance", r.f.nativeEvent(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "start", ThreadID: "session", TurnID: "turn", Status: "interrupted"})); e != nil {
		return e
	}
	return r.f.coordinator.RegisterCommand(ctx, carrier, command)
}
func TestIndependentTerminalBetweenResponseSaveAndRegisterMustNotWrite(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	f.driver.BindRegistrar(reviewTerminalRegistrar{f})
	id, err := f.service.Respond(t.Context(), f.decision)
	if err == nil {
		t.Fatal("stale response was reported as sent")
	}
	if f.writes != 0 {
		t.Fatalf("approval/cancel response was written after its exact native turn had durably ended: %d", f.writes)
	}
	f.tx(func(tx *workstore.Tx) error {
		request, err := tx.GetNativeRequest(f.request.ID)
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		if request.State != "stale" || request.ResolvedEvidenceRef == "" || op.State != work.OperationTerminal || op.NextAttemptAt != nil {
			t.Fatalf("provably unsent terminal response remained retryable: request=%s operation=%s", request.State, op.State)
		}
		return nil
	})
}
