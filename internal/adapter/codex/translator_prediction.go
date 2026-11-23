package codex

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/jsonrpcutil"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

// PredictionOptions comes from the same authenticated configuration as the desktop.
// There is deliberately no locally authored prediction prompt.
type PredictionOptions struct {
	Prompt            string
	PromptWithSchema  string
	Effort            *string
	InterruptOnCancel bool
	GenerationTrigger string
}

const predictionSuggestionSchema = `{"type":"object","properties":{"suggestion":{"type":["string","null"]}},"required":["suggestion"],"additionalProperties":false}`

type pendingPrediction struct {
	CommandID    string
	ThreadID     string
	SourceTurnID string
}

func (t *Translator) TranslatePredictionRequest(command agentproto.Command, options PredictionOptions) ([][]byte, error) {
	prompt := options.PromptWithSchema
	if xutil.TrimECMAScriptSpace(prompt) == "" {
		if xutil.TrimECMAScriptSpace(options.Prompt) == "" {
			return nil, fmt.Errorf("prediction requires official desktop configuration")
		}
		prompt = options.Prompt + "\n\nReturn only JSON matching this schema: " + predictionSuggestionSchema + "."
	}
	commandID := strings.TrimSpace(command.CommandID)
	threadID := strings.TrimSpace(command.Target.ThreadID)
	sourceTurnID := strings.TrimSpace(command.Prediction.SourceTurnID)
	if commandID == "" || threadID == "" || sourceTurnID == "" {
		return nil, fmt.Errorf("thread.prediction.request requires command, thread and source turn id")
	}
	for _, pending := range t.pendingPredictions {
		if pending.CommandID == commandID {
			return nil, fmt.Errorf("prediction command %s is already pending", commandID)
		}
	}
	requestID := t.predictionRequestPrefix + strconv.Itoa(t.NextID())
	params := map[string]any{
		"threadId": threadID, "expectedTurnId": sourceTurnID,
		"prompt":       prompt,
		"effort":       options.Effort,
		"threadSource": "composer_predictions", "turnTrigger": "composer_predictions",
		"responsesapiClientMetadata": map[string]string{
			"prediction_source_thread_id": threadID,
			"prediction_source_turn_id":   sourceTurnID,
			"prediction_trigger":          options.GenerationTrigger,
		},
		"disableMcp": true,
	}
	if options.InterruptOnCancel {
		params["interruptOnCancel"] = true
	}
	raw, err := json.Marshal(map[string]any{
		"id": requestID, "method": "thread/sample", "params": params,
	})
	if err != nil {
		return nil, err
	}
	t.pendingPredictions[requestID] = pendingPrediction{
		CommandID: commandID, ThreadID: threadID, SourceTurnID: sourceTurnID,
	}
	return [][]byte{append(raw, '\n')}, nil
}

// DiscardUnsentPrediction removes ownership of a sample the native write queue rejected.
func (t *Translator) DiscardUnsentPrediction(commandID string) {
	commandID = strings.TrimSpace(commandID)
	for requestID, pending := range t.pendingPredictions {
		if pending.CommandID == commandID {
			delete(t.pendingPredictions, requestID)
			return
		}
	}
}

func (t *Translator) translatePredictionCancel(command agentproto.Command) ([][]byte, error) {
	commandID := strings.TrimSpace(command.Prediction.RequestCommandID)
	if commandID == "" {
		return nil, fmt.Errorf("thread.prediction.cancel requires original request command id")
	}
	for requestID, pending := range t.pendingPredictions {
		if pending.CommandID != commandID {
			continue
		}
		cancelID := t.predictionRequestPrefix + strconv.Itoa(t.NextID())
		raw, err := json.Marshal(map[string]any{
			"id": cancelID, "method": "thread/sample/cancel",
			"params": map[string]any{"requestId": requestID},
		})
		if err != nil {
			return nil, err
		}
		delete(t.pendingPredictions, requestID)
		return [][]byte{append(raw, '\n')}, nil
	}
	return nil, nil
}

func (t *Translator) observePredictionResponse(requestID string, message map[string]any) (Result, bool) {
	if method, _ := message["method"].(string); method != "" || !strings.HasPrefix(requestID, t.predictionRequestPrefix) {
		return Result{}, false
	}
	pending, active := t.pendingPredictions[requestID]
	if !active {
		// The private translator prefix covers cancel acknowledgements and
		// late sample responses even after dropping their request state.
		return Result{Suppress: true}, true
	}
	delete(t.pendingPredictions, requestID)
	event := agentproto.Event{
		Kind: agentproto.EventThreadPredictionResult, CommandID: pending.CommandID,
		ThreadID:   pending.ThreadID,
		Prediction: &agentproto.PredictionResult{SourceTurnID: pending.SourceTurnID},
	}
	if message["error"] != nil {
		code, _ := lookupMap(message, "error")["code"].(float64)
		event.ErrorMessage = jsonrpcutil.ExtractErrorMessage(message)
		event.Prediction.Unsupported = code == -32601 ||
			(code == -32600 && strings.HasPrefix(event.ErrorMessage, "Invalid request: unknown variant `thread/sample`,"))
		if event.ErrorMessage == "" {
			event.ErrorMessage = "prediction sample failed"
		}
	} else {
		result := lookupMap(message, "result")
		text, _ := result["text"].(string)
		if strings.TrimSpace(text) != "" {
			event.Prediction.SourceTurnID, _ = result["sourceTurnId"].(string)
			if event.Prediction.SourceTurnID != pending.SourceTurnID {
				event.ErrorMessage = "prediction source turn does not match the request"
			}
		}
		if event.ErrorMessage == "" {
			suggestion, err := parsePredictionSuggestion(text)
			if err != nil {
				event.ErrorMessage = err.Error()
			} else {
				event.Prediction.Suggestion = suggestion
				if t.suppressPredictionByModeration(pending.ThreadID, pending.SourceTurnID) {
					event.Prediction.Suggestion = ""
				}
			}
		}
	}
	return Result{Suppress: true, Events: []agentproto.Event{event}}, true
}

func parsePredictionSuggestion(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil || object == nil {
		return "", fmt.Errorf("prediction JSON must be an object")
	}
	raw, exists := object["suggestion"]
	if !exists {
		return "", fmt.Errorf("prediction JSON requires the suggestion key")
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("prediction JSON requires a suggestion string or null")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("prediction JSON must contain one object")
	}
	if value == nil {
		return "", nil
	}
	return xutil.TrimECMAScriptSpace(*value), nil
}
