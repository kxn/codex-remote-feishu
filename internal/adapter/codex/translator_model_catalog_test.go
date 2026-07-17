package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTranslateModelListCommand(t *testing.T) {
	tr := NewTranslator("inst-1")

	payloads, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-models-1",
		Kind:      agentproto.CommandModelList,
		ModelList: agentproto.ModelListCommand{
			Limit: 25,
		},
	})
	if err != nil {
		t.Fatalf("TranslateCommand returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one payload, got %d", len(payloads))
	}

	var payload map[string]any
	if err := json.Unmarshal(payloads[0], &payload); err != nil {
		t.Fatalf("payload is not valid json: %v", err)
	}
	if payload["method"] != "model/list" {
		t.Fatalf("method = %v, want model/list", payload["method"])
	}
	params, ok := payload["params"].(map[string]any)
	if !ok {
		t.Fatalf("params missing: %#v", payload)
	}
	if params["includeHidden"] != false || params["limit"] != float64(25) {
		t.Fatalf("unexpected params: %#v", params)
	}
}

func TestModelListResponseEmitsCatalogUpdated(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-models-1",
		Kind:      agentproto.CommandModelList,
	}); err != nil {
		t.Fatalf("TranslateCommand returned error: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{
		"id":"relay-model-list-0",
		"result":{
			"data":[
				{
					"id":"m1",
					"model":"gpt-5.5",
					"displayName":"GPT 5.5",
					"description":"Fast model",
					"hidden":false,
					"supportedReasoningEfforts":[
						{"reasoningEffort":"low","description":"Low"},
						{"reasoningEffort":"high","description":"High"}
					],
					"defaultReasoningEffort":"high",
					"serviceTiers":[{"id":"priority","name":"Priority","description":"Fast lane"}],
					"defaultServiceTier":"priority",
					"upgrade":"recommended",
					"upgradeInfo":{"model":"gpt-5.6","upgradeCopy":"Try newer","modelLink":"https://example.com/model","migrationMarkdown":"Move"},
					"availabilityNux":{"message":"Available now"},
					"isDefault":true
				},
				{
					"id":"hidden",
					"model":"hidden-model",
					"displayName":"Hidden",
					"hidden":true,
					"supportedReasoningEfforts":[],
					"defaultReasoningEffort":"medium",
					"isDefault":false
				}
			],
			"nextCursor":"next-1"
		}
	}`))
	if err != nil {
		t.Fatalf("ObserveServer returned error: %v", err)
	}
	if !result.Suppress || len(result.Events) != 1 {
		t.Fatalf("expected one suppressed event, got %#v", result)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventModelCatalogUpdated || event.CommandID != "cmd-models-1" {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event.ModelCatalog == nil {
		t.Fatalf("expected model catalog payload")
	}
	if event.ModelCatalog.NextCursor != "next-1" || event.ModelCatalog.Unsupported || event.ModelCatalog.ErrorMessage != "" {
		t.Fatalf("unexpected catalog status: %#v", event.ModelCatalog)
	}
	if len(event.ModelCatalog.Entries) != 2 {
		t.Fatalf("expected two entries, got %#v", event.ModelCatalog.Entries)
	}
	first := event.ModelCatalog.Entries[0]
	if first.ID != "m1" || first.Model != "gpt-5.5" || first.DisplayName != "GPT 5.5" || first.Hidden || !first.IsDefault {
		t.Fatalf("unexpected first entry: %#v", first)
	}
	if len(first.SupportedReasoningEfforts) != 2 || first.SupportedReasoningEfforts[0].ReasoningEffort != "low" || first.SupportedReasoningEfforts[1].ReasoningEffort != "high" {
		t.Fatalf("reasoning efforts order not preserved: %#v", first.SupportedReasoningEfforts)
	}
	if first.DefaultReasoningEffort != "high" || len(first.ServiceTiers) != 1 || first.ServiceTiers[0].ID != "priority" || first.DefaultServiceTier != "priority" {
		t.Fatalf("unexpected model metadata: %#v", first)
	}
	if first.UpgradeInfo == nil || first.UpgradeInfo.Model != "gpt-5.6" || first.AvailabilityMessage != "Available now" {
		t.Fatalf("expected upgrade and availability metadata, got %#v", first)
	}
}

func TestModelListMethodNotFoundEmitsUnsupportedCatalog(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-models-1",
		Kind:      agentproto.CommandModelList,
	}); err != nil {
		t.Fatalf("TranslateCommand returned error: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-model-list-0","error":{"code":-32601,"message":"Method not found"}}`))
	if err != nil {
		t.Fatalf("ObserveServer returned error: %v", err)
	}
	if !result.Suppress || len(result.Events) != 1 {
		t.Fatalf("expected one suppressed unsupported event, got %#v", result)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventModelCatalogUpdated || event.ModelCatalog == nil {
		t.Fatalf("unexpected event: %#v", event)
	}
	if !event.ModelCatalog.Unsupported || event.ModelCatalog.ErrorMessage == "" {
		t.Fatalf("expected unsupported catalog event, got %#v", event.ModelCatalog)
	}
}
