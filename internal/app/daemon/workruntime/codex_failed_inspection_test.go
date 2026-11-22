package workruntime

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestCodexFailedModelTurnCanProveStoppedWithoutIdleStatus(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	var request InspectionRequest
	f.tx(func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		request, err = f.coordinator.inspectionRequest(tx, attempt)
		return err
	})
	for _, tc := range []struct {
		name, turns string
		pending     bool
		stopped     bool
	}{
		{"actual modelNotFound failure", fmt.Sprintf(`[{"id":%q,"status":"failed","error":{"message":"The selected model is not supported when using Codex with a ChatGPT account.","codexErrorInfo":"modelNotFound"}}]`, request.TurnID), false, true},
		{"no failed turn evidence", `[]`, false, false},
		{"wrong turn", `[{"id":"other","status":"failed"}]`, false, false},
		{"still active turn", fmt.Sprintf(`[{"id":%q,"status":"failed"},{"id":"later","status":"inProgress"}]`, request.TurnID), false, false},
		{"unknown terminal", fmt.Sprintf(`[{"id":%q,"status":"future"}]`, request.TurnID), false, false},
		{"pending native input", fmt.Sprintf(`[{"id":%q,"status":"failed"}]`, request.TurnID), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := codexInspection(request)
			input.Native.Response = json.RawMessage(fmt.Sprintf(`{"id":"inspect-native","result":{"thread":{"id":%q,"status":{"type":"systemError"},"turns":%s}}}`, request.SessionID, tc.turns))
			if tc.pending {
				input.Background.PendingNativeInputs = 1
			}
			observation, err := ObserveProtocol(request, input)
			if err != nil {
				t.Fatal(err)
			}
			if observation.provesStopped(request) != tc.stopped {
				t.Fatalf("stopped=%v want %v: %+v", observation.provesStopped(request), tc.stopped, observation)
			}
		})
	}
}
