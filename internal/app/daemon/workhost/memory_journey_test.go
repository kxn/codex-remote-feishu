package workhost

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestCorrectedPrivateMemoryRefreshesCarrierAndRetainsNativeHistory(t *testing.T) {
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { return 1234, nil })
	h.config.Runtime = &rebuildDriver{stopped: true}
	h.config.StopCarrier = func(context.Context, work.WorkerSession) error { return nil }
	writeMemory := func(revision int64, body, applicability string) {
		t.Helper()
		err := store.Within(t.Context(), func(tx *workstore.Tx) error {
			content := work.NewContent(body+":content", "scope", "text/plain", []byte(body), time.Now())
			if e := tx.PutContent(content); e != nil {
				return e
			}
			message := work.Message{ID: body + ":message", ScopeID: "scope", ConversationID: "private", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, BodyRef: content.ID, Revision: 1}
			if _, _, e := tx.AcceptMessage(message, nil); e != nil {
				return e
			}
			return tx.PutMemory(work.MemoryEntry{ID: "preference", StorageScopeID: "scope", Scope: work.MemoryScope{Kind: work.MemoryPersonal, RoleID: "role", PrincipalID: "human"}, Author: work.Actor{Kind: work.ActorRole, ID: "role"}, Sources: []work.SourceRef{{Kind: "message", ID: message.ID, Version: "1", EvidenceRef: content.ID}}, ContentRef: content.ID, Applicability: applicability, State: work.MemoryValid, Revision: revision, UpdatedAt: time.Now()}, revision-1)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	writeMemory(1, "previous design rule", "this person's prototype")
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := h.Prepare(t.Context(), need); err != nil {
		t.Fatal(err)
	}
	old := seedIdleSession(t, h, store)
	writeMemory(2, "corrected design rule", "only mobile clients on slow networks")
	candidate := readLifecycleCandidate(t, store, old.ContextID)
	if _, err := h.Plan(t.Context(), candidate); !errors.Is(err, workengine.ErrPreparationRequired) {
		t.Fatalf("stale native history was reusable: %v", err)
	}
	if err := h.Prepare(t.Context(), need); err != nil {
		t.Fatal(err)
	}
	current := readyLifecycleCandidate(t, store, old.ContextID)
	if current.Worker.Generation != old.Generation+1 || current.Worker.NativeSessionID != old.NativeSessionID || current.Worker.CarrierIncarnation == old.CarrierIncarnation {
		t.Fatal("corrected private memory lost native history or failed to refresh carrier")
	}
	var launch LaunchRecord
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error { var e error; launch, e = h.readLaunch(tx, current.Worker); return e }); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"corrected design rule", "only mobile clients on slow networks"} {
		if !strings.Contains(launch.Snapshot.InstructionBundle.Text, want) {
			t.Fatalf("new native instructions lost %q", want)
		}
	}
	if strings.Contains(launch.Snapshot.InstructionBundle.Text, "previous design rule") {
		t.Fatal("obsolete memory leaked into refreshed instructions")
	}
	plan, err := h.Plan(t.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Command.Prompt.Inputs[0].Text, "completed original prompt") {
		t.Fatal("resumed private session discarded durable work checkpoint")
	}
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		inputs, e := tx.ListPendingInputs("scope", "role")
		if e != nil {
			return e
		}
		for _, input := range inputs {
			if input.RecipientContextID == old.ContextID && strings.HasPrefix(input.MessageID, "memory-correction:") {
				return nil
			}
		}
		return errors.New("memory correction did not wake the original work")
	}); err != nil {
		t.Fatal(err)
	}
}
