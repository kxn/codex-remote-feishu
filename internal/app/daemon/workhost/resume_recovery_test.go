package workhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func assertMissingPoolResumeRebuildsOnce(t *testing.T, host *Host, store *workstore.Store, candidate workengine.Candidate, need workengine.WorkerNeed) {
	t.Helper()
	worker := candidate.Worker
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutInbox(work.Inbox{ID: "next-input", ScopeID: worker.ScopeID, RecipientRoleID: worker.RoleID, RecipientContextID: worker.ContextID, EventID: "follow-up", State: work.InboxPending, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	coordinator := workruntime.New(store, workruntime.Options{Driver: host.config.Runtime})
	var scheduled []agentproto.Command
	dispatcher := lifecycleDispatcher{func(ctx context.Context, instance string, command agentproto.Command) error {
		scheduled = append(scheduled, command)
		if len(scheduled) > 1 {
			return nil
		}
		if err := coordinator.RegisterCommand(ctx, worker.CarrierIncarnation, command); err != nil {
			return err
		}
		raw, err := json.Marshal(command)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		receipt := &agentproto.RuntimeCommandReceipt{Version: agentproto.JournalProtocolVersion, CommandID: command.CommandID, PayloadHash: hex.EncodeToString(sum[:]), CarrierIncarnation: worker.CarrierIncarnation, Stage: "prepared"}
		return coordinator.HandleAck(ctx, instance, agentproto.CommandAck{CommandID: command.CommandID, RuntimeReceipt: receipt, Problem: &agentproto.ErrorInfo{Code: "claude_resume_thread_not_found"}})
	}}
	engine, err := workengine.New(store, workengine.Config{MaxConcurrent: 1}, host, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	tick := func() {
		result, err := engine.Tick(t.Context())
		if err != nil || len(result.Dispatched) != 1 || result.Dispatched[0].Err != nil {
			t.Fatalf("expected one dispatch: %+v, %v", result, err)
		}
	}
	tick()
	if err := host.Prepare(t.Context(), need); err != nil {
		t.Fatal(err)
	}
	ready := readyLifecycleCandidate(t, store, worker.ContextID).Worker
	if ready.Generation != worker.Generation+1 || ready.NativeSessionID != "" {
		t.Fatalf("verified missing history did not trigger one fresh rebuild: %+v", ready)
	}
	tick()
	fresh := scheduled[1]
	if fresh.Target.EffectivePromptExecutionMode() != agentproto.PromptExecutionModeStartNew || fresh.Target.ThreadID != "" || fresh.Execution.RunRef == scheduled[0].Execution.RunRef {
		t.Fatalf("recovery repeated the unavailable resume: %+v", fresh)
	}
	if text := fresh.Prompt.Inputs[0].Text; !strings.Contains(text, "completed original prompt") || !strings.Contains(text, "next-input") || !strings.Contains(text, "已核验原生会话无法恢复") {
		t.Fatalf("fresh attempt lost original input or checkpoint: %q", text)
	}
}
