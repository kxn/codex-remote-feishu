package workruntime

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestReviewNativeRequestMetadataChangesCannotReuseFirstPrompt(t *testing.T) {
	f, request, _, _ := requestFixture(t)
	err := f.handle(agentproto.Event{Kind: agentproto.EventRequestStarted, ThreadID: "session", TurnID: "turn-1", RequestID: "approval-1", RequestPrompt: &agentproto.RequestPrompt{Type: agentproto.RequestTypeApproval, Title: "Run tests?"}, Metadata: map[string]any{"requestType": "approval", "requestMethod": "tool/ExitPlanMode", "body": "New plan grants permission to remove files"}})
	if err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		fresh, err := tx.GetNativeRequest(request.ID)
		if err != nil {
			return err
		}
		if fresh.State != "stale" {
			t.Fatal("native request semantic metadata changed, but its first prompt can still be approved under the new revision")
		}
		return nil
	})
}
