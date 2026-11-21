package wrapper

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestManagedClaudeCatalogUsesActualInitializeModelsWithoutAccountData(t *testing.T) {
	app := newTestWorkApp(t, agentproto.BackendClaude)
	app.work.inspection.bootstrap = agentproto.RPCProbe{Request: json.RawMessage(`{"type":"control_request","request_id":"initialize-proof","request":{"subtype":"initialize"}}`), Response: json.RawMessage(`{"type":"control_response","response":{"subtype":"success","request_id":"initialize-proof","response":{"account":{"email":"do-not-copy-account"},"models":[{"value":"opus","resolvedModel":"claude-native-opus","displayName":"Native Opus","supportedEffortLevels":["low","high"]}]}}}`)}
	owner := app.work.snapshot.Owner
	command := agentproto.Command{CommandID: "models", Kind: agentproto.CommandModelList, Execution: &owner}
	result, err := app.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Execution == nil || result.Events[0].Execution.AttemptID != "" || result.Events[0].RequestID != "initialize-proof" || result.Events[0].ModelCatalog.Entries[0].Model != "opus" || len(result.Events[0].ModelCatalog.Entries[0].SupportedReasoningEfforts) != 2 {
		t.Fatalf("native model proof lost: %+v", result)
	}
	raw, _ := json.Marshal(result.Events)
	if strings.Contains(string(raw), "do-not-copy-account") {
		t.Fatal("account data leaked into model catalog event")
	}
}
