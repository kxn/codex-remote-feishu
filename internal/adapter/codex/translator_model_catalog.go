package codex

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Translator) translateModelList(command agentproto.Command) ([][]byte, error) {
	requestID := t.nextRequest("model-list")
	params := map[string]any{
		"includeHidden": command.ModelList.IncludeHidden,
	}
	if cursor := strings.TrimSpace(command.ModelList.Cursor); cursor != "" {
		params["cursor"] = cursor
	}
	if command.ModelList.Limit > 0 {
		params["limit"] = command.ModelList.Limit
	}
	t.pendingModelList[requestID] = pendingModelList{
		CommandID:     command.CommandID,
		IncludeHidden: command.ModelList.IncludeHidden,
	}
	t.debugf(
		"translate model list: command=%s request=%s includeHidden=%t limit=%d cursor=%s",
		command.CommandID,
		requestID,
		command.ModelList.IncludeHidden,
		command.ModelList.Limit,
		strings.TrimSpace(command.ModelList.Cursor),
	)
	payload := map[string]any{
		"id":     requestID,
		"method": "model/list",
		"params": params,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return [][]byte{append(bytes, '\n')}, nil
}

func (t *Translator) observeModelListResponse(requestID string, message map[string]any) (Result, bool) {
	pending, exists := t.pendingModelList[requestID]
	if !exists {
		return Result{}, false
	}
	delete(t.pendingModelList, requestID)
	snapshot := agentproto.ModelCatalogSnapshot{
		IncludeHidden: pending.IncludeHidden,
		RefreshedAt:   time.Now().UTC(),
	}
	if errMsg := extractJSONRPCErrorMessage(message); errMsg != "" {
		snapshot.ErrorMessage = errMsg
		snapshot.Unsupported = isModelListUnsupportedError(message, errMsg)
		t.debugf(
			"observe server model/list error: request=%s command=%s unsupported=%t error=%s",
			requestID,
			pending.CommandID,
			snapshot.Unsupported,
			errMsg,
		)
		return Result{
			Suppress: true,
			Events: []agentproto.Event{{
				Kind:         agentproto.EventModelCatalogUpdated,
				CommandID:    pending.CommandID,
				ModelCatalog: &snapshot,
			}},
		}, true
	}
	result, _ := message["result"].(map[string]any)
	if result == nil {
		snapshot.ErrorMessage = "model/list response missing result"
	} else {
		snapshot = parseModelCatalogSnapshot(result, pending.IncludeHidden)
	}
	t.debugf(
		"observe server model/list result: request=%s command=%s entries=%d nextCursor=%s error=%s",
		requestID,
		pending.CommandID,
		len(snapshot.Entries),
		snapshot.NextCursor,
		snapshot.ErrorMessage,
	)
	return Result{
		Suppress: true,
		Events: []agentproto.Event{{
			Kind:         agentproto.EventModelCatalogUpdated,
			CommandID:    pending.CommandID,
			ModelCatalog: &snapshot,
		}},
	}, true
}

func isModelListUnsupportedError(message map[string]any, errMsg string) bool {
	if code := lookupAny(message, "error", "code"); code != nil {
		switch value := code.(type) {
		case float64:
			if int(value) == -32601 {
				return true
			}
		case int:
			if value == -32601 {
				return true
			}
		case int64:
			if value == -32601 {
				return true
			}
		case string:
			if strings.TrimSpace(value) == "-32601" {
				return true
			}
		}
	}
	normalized := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(normalized, "method not found") ||
		strings.Contains(normalized, "unknown method") ||
		strings.Contains(normalized, "method_not_found")
}

func parseModelCatalogSnapshot(result map[string]any, includeHidden bool) agentproto.ModelCatalogSnapshot {
	snapshot := agentproto.ModelCatalogSnapshot{
		IncludeHidden: includeHidden,
		RefreshedAt:   time.Now().UTC(),
		NextCursor: firstNonEmptyString(
			lookupStringFromAny(result["nextCursor"]),
			lookupStringFromAny(result["next_cursor"]),
		),
	}
	for _, raw := range lookupSliceAny(result, "data") {
		entry, ok := parseModelCatalogEntry(raw)
		if !ok {
			continue
		}
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	return snapshot
}

func parseModelCatalogEntry(raw any) (agentproto.ModelCatalogEntry, bool) {
	item, ok := raw.(map[string]any)
	if !ok {
		return agentproto.ModelCatalogEntry{}, false
	}
	entry := agentproto.ModelCatalogEntry{
		ID: firstNonEmptyString(
			lookupStringFromAny(item["id"]),
			lookupStringFromAny(item["ID"]),
		),
		Model: firstNonEmptyString(
			lookupStringFromAny(item["model"]),
			lookupStringFromAny(item["name"]),
		),
		DisplayName: firstNonEmptyString(
			lookupStringFromAny(item["displayName"]),
			lookupStringFromAny(item["display_name"]),
		),
		Description: firstNonEmptyString(
			lookupStringFromAny(item["description"]),
			lookupStringFromAny(item["summary"]),
		),
		Hidden: lookupBoolFromAny(firstNonNil(item["hidden"], item["isHidden"], item["is_hidden"])),
		DefaultReasoningEffort: firstNonEmptyString(
			lookupStringFromAny(item["defaultReasoningEffort"]),
			lookupStringFromAny(item["default_reasoning_effort"]),
		),
		DefaultServiceTier: firstNonEmptyString(
			lookupStringFromAny(item["defaultServiceTier"]),
			lookupStringFromAny(item["default_service_tier"]),
		),
		Upgrade: firstNonEmptyString(
			lookupStringFromAny(item["upgrade"]),
			lookupStringFromAny(item["upgradeStatus"]),
			lookupStringFromAny(item["upgrade_status"]),
		),
		AvailabilityMessage: parseAvailabilityMessage(firstNonNil(item["availabilityNux"], item["availability_nux"])),
		IsDefault:           lookupBoolFromAny(firstNonNil(item["isDefault"], item["is_default"])),
	}
	entry.SupportedReasoningEfforts = parseReasoningEffortOptions(firstNonNil(item["supportedReasoningEfforts"], item["supported_reasoning_efforts"]))
	entry.ServiceTiers = parseModelServiceTiers(firstNonNil(item["serviceTiers"], item["service_tiers"]))
	entry.UpgradeInfo = parseModelUpgradeInfo(firstNonNil(item["upgradeInfo"], item["upgrade_info"]))
	if entry.Model == "" && entry.ID == "" {
		return agentproto.ModelCatalogEntry{}, false
	}
	return entry, true
}

func parseReasoningEffortOptions(raw any) []agentproto.ReasoningEffortOption {
	items := sliceAnyFromAny(raw)
	options := make([]agentproto.ReasoningEffortOption, 0, len(items))
	for _, rawItem := range items {
		switch value := rawItem.(type) {
		case string:
			effort := strings.TrimSpace(value)
			if effort != "" {
				options = append(options, agentproto.ReasoningEffortOption{ReasoningEffort: effort})
			}
		case map[string]any:
			effort := firstNonEmptyString(
				lookupStringFromAny(value["reasoningEffort"]),
				lookupStringFromAny(value["reasoning_effort"]),
				lookupStringFromAny(value["id"]),
				lookupStringFromAny(value["value"]),
			)
			if effort == "" {
				continue
			}
			options = append(options, agentproto.ReasoningEffortOption{
				ReasoningEffort: effort,
				Description: firstNonEmptyString(
					lookupStringFromAny(value["description"]),
					lookupStringFromAny(value["label"]),
				),
			})
		}
	}
	return options
}

func parseModelServiceTiers(raw any) []agentproto.ModelServiceTier {
	items := sliceAnyFromAny(raw)
	tiers := make([]agentproto.ModelServiceTier, 0, len(items))
	for _, rawItem := range items {
		switch value := rawItem.(type) {
		case string:
			id := strings.TrimSpace(value)
			if id != "" {
				tiers = append(tiers, agentproto.ModelServiceTier{ID: id})
			}
		case map[string]any:
			tier := agentproto.ModelServiceTier{
				ID: firstNonEmptyString(
					lookupStringFromAny(value["id"]),
					lookupStringFromAny(value["value"]),
				),
				Name: firstNonEmptyString(
					lookupStringFromAny(value["name"]),
					lookupStringFromAny(value["label"]),
				),
				Description: lookupStringFromAny(value["description"]),
			}
			if tier.ID != "" || tier.Name != "" {
				tiers = append(tiers, tier)
			}
		}
	}
	return tiers
}

func parseModelUpgradeInfo(raw any) *agentproto.ModelUpgradeInfo {
	item, ok := raw.(map[string]any)
	if !ok || item == nil {
		return nil
	}
	info := &agentproto.ModelUpgradeInfo{
		Model: firstNonEmptyString(
			lookupStringFromAny(item["model"]),
			lookupStringFromAny(item["targetModel"]),
			lookupStringFromAny(item["target_model"]),
		),
		UpgradeCopy: firstNonEmptyString(
			lookupStringFromAny(item["upgradeCopy"]),
			lookupStringFromAny(item["upgrade_copy"]),
		),
		ModelLink: firstNonEmptyString(
			lookupStringFromAny(item["modelLink"]),
			lookupStringFromAny(item["model_link"]),
		),
		MigrationMarkdown: firstNonEmptyString(
			lookupStringFromAny(item["migrationMarkdown"]),
			lookupStringFromAny(item["migration_markdown"]),
		),
	}
	if info.Model == "" && info.UpgradeCopy == "" && info.ModelLink == "" && info.MigrationMarkdown == "" {
		return nil
	}
	return info
}

func parseAvailabilityMessage(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		return firstNonEmptyString(
			lookupStringFromAny(value["message"]),
			lookupStringFromAny(value["title"]),
			lookupStringFromAny(value["description"]),
		)
	default:
		return ""
	}
}

func lookupSliceAny(value map[string]any, key string) []any {
	if value == nil {
		return nil
	}
	return sliceAnyFromAny(value[key])
}

func sliceAnyFromAny(raw any) []any {
	switch value := raw.(type) {
	case []any:
		return value
	case []map[string]any:
		items := make([]any, 0, len(value))
		for _, item := range value {
			items = append(items, item)
		}
		return items
	case []string:
		items := make([]any, 0, len(value))
		for _, item := range value {
			items = append(items, item)
		}
		return items
	default:
		return nil
	}
}
