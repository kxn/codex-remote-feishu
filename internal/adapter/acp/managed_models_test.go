package acp

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestManagedModelDiscoveryCreatesOnlyNativeMetadataSession(t *testing.T) {
	translator := NewTranslator("instance", "/workspace")
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	command := agentproto.Command{CommandID: "models", Kind: agentproto.CommandModelList, Execution: &owner}
	result, err := translator.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.OutboundToChild) != 1 {
		t.Fatal("native model discovery did not issue metadata RPC")
	}
	var request map[string]any
	if err = json.Unmarshal(result.OutboundToChild[0], &request); err != nil {
		t.Fatal(err)
	}
	if request["method"] != "session/new" {
		t.Fatal("metadata discovery used model prompt")
	}
	response, _ := json.Marshal(map[string]any{"id": request["id"], "result": map[string]any{"sessionId": "metadata-session", "configOptions": []any{map[string]any{"id": "model", "category": "model", "type": "select", "currentValue": "provider/model", "options": []any{map[string]any{"value": "provider/model", "name": "Native model"}}}}}})
	observed, err := translator.ObserveServer(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.OutboundToChild) != 0 || len(observed.Events) != 1 || observed.Events[0].Kind != agentproto.EventModelCatalogUpdated || observed.Events[0].CommandID != "models" || observed.Events[0].RequestID == "" || observed.Events[0].ModelCatalog == nil || observed.Events[0].ModelCatalog.Unsupported || observed.Events[0].ModelCatalog.Entries[0].Model != "provider/model" {
		t.Fatalf("metadata session became a prompt or lost exact response: %+v", observed)
	}
	if len(translator.activeTurns) != 0 {
		t.Fatal("model discovery invented a native work turn")
	}
}
