package wrapper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (w *workExecutionState) claudeModels(command agentproto.Command) agentproto.Event {
	w.inspection.mu.Lock()
	bootstrap := w.inspection.bootstrap
	w.inspection.mu.Unlock()
	catalog := &agentproto.ModelCatalogSnapshot{RefreshedAt: time.Now().UTC(), IncludeHidden: command.ModelList.IncludeHidden}
	event := agentproto.Event{Kind: agentproto.EventModelCatalogUpdated, CommandID: command.CommandID, ModelCatalog: catalog}
	var response struct {
		Response struct {
			ID       string `json:"request_id"`
			Subtype  string `json:"subtype"`
			Response struct {
				Models []struct {
					Value, ResolvedModel, DisplayName, Description string
					SupportedEffortLevels                          []string
				}
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(bootstrap.Response, &response) != nil || response.Response.Subtype != "success" || response.Response.ID == "" || len(response.Response.Response.Models) == 0 {
		catalog.Unsupported = true
		catalog.ErrorMessage = "Claude initialize did not expose a native model catalog"
		return event
	}
	event.RequestID = response.Response.ID
	for _, model := range response.Response.Response.Models {
		if model.Value == "" {
			continue
		}
		entry := agentproto.ModelCatalogEntry{ID: model.Value, Model: model.Value, DisplayName: model.DisplayName, Description: model.Description, IsDefault: model.Value == "default"}
		for _, effort := range model.SupportedEffortLevels {
			entry.SupportedReasoningEfforts = append(entry.SupportedReasoningEfforts, agentproto.ReasoningEffortOption{ReasoningEffort: effort})
		}
		catalog.Entries = append(catalog.Entries, entry)
	}
	sum := sha256.Sum256(bootstrap.Response)
	event.Metadata = map[string]any{"source": "claude.initialize.models", "native_response_sha256": hex.EncodeToString(sum[:])}
	return event
}
