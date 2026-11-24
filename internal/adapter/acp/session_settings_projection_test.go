package acp

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestSessionReadyAndEffortUpdatePublishCompleteSettingsAndCatalog(t *testing.T) {
	tr := NewTranslator("instance", "/workspace")
	request, err := tr.TranslateCommand(agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, Origin: agentproto.Origin{Surface: "surface"}, Target: agentproto.Target{ExecutionMode: agentproto.PromptExecutionModeStartNew, CWD: "/workspace"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}})
	if err != nil {
		t.Fatal(err)
	}
	frame := decodeFrame(t, request.OutboundToChild[0])
	options := []any{map[string]any{"id": "model", "type": "select", "currentValue": "provider/model", "options": []any{map[string]any{"value": "provider/model", "name": "Model"}}}, map[string]any{"id": "effort", "type": "select", "currentValue": "low", "options": []any{map[string]any{"value": "low", "name": "Low"}, map[string]any{"value": "high", "name": "High"}}}}
	result, err := tr.ObserveServer(mustLine(t, map[string]any{"id": frame["id"], "result": map[string]any{"sessionId": "session", "configOptions": options}}))
	if err != nil {
		t.Fatal(err)
	}
	var settings *agentproto.ThreadSettingsUpdate
	var catalog *agentproto.ModelCatalogSnapshot
	for _, event := range result.Events {
		if event.ThreadSettings != nil {
			settings = event.ThreadSettings
		}
		if event.ModelCatalog != nil {
			catalog = event.ModelCatalog
		}
	}
	if settings == nil || settings.Model != "provider/model" || settings.ReasoningEffort != "low" || catalog == nil || len(catalog.Entries) != 1 || len(catalog.Entries[0].SupportedReasoningEfforts) != 2 {
		t.Fatalf("ready settings=%#v catalog=%#v", settings, catalog)
	}
	update := sessionUpdate("session", map[string]any{"sessionUpdate": "config_option_update", "configOption": map[string]any{"id": "effort", "type": "select", "currentValue": "high", "options": []any{map[string]any{"value": "low"}, map[string]any{"value": "high"}}}})
	result, err = tr.ObserveServer(mustLine(t, update))
	if err != nil {
		t.Fatal(err)
	}
	settings = nil
	for _, event := range result.Events {
		if event.ThreadSettings != nil {
			settings = event.ThreadSettings
		}
	}
	if settings == nil || settings.Model != "provider/model" || settings.ReasoningEffort != "high" {
		t.Fatalf("effort update lost current model: %#v", settings)
	}
}
