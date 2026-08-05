package codex

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
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
		NextCursor: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(result["nextCursor"]),
			xutil.LookupStringFromAny(result["next_cursor"]),
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
		ID: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["id"]),
			xutil.LookupStringFromAny(item["ID"]),
		),
		Model: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["model"]),
			xutil.LookupStringFromAny(item["name"]),
		),
		DisplayName: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["displayName"]),
			xutil.LookupStringFromAny(item["display_name"]),
		),
		Description: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["description"]),
			xutil.LookupStringFromAny(item["summary"]),
		),
		Hidden: xutil.LookupBoolFromAny(firstNonNil(item["hidden"], item["isHidden"], item["is_hidden"])),
		DefaultReasoningEffort: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["defaultReasoningEffort"]),
			xutil.LookupStringFromAny(item["default_reasoning_effort"]),
		),
		DefaultServiceTier: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["defaultServiceTier"]),
			xutil.LookupStringFromAny(item["default_service_tier"]),
		),
		Upgrade: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["upgrade"]),
			xutil.LookupStringFromAny(item["upgradeStatus"]),
			xutil.LookupStringFromAny(item["upgrade_status"]),
		),
		AvailabilityMessage: parseAvailabilityMessage(firstNonNil(item["availabilityNux"], item["availability_nux"])),
		IsDefault:           xutil.LookupBoolFromAny(firstNonNil(item["isDefault"], item["is_default"])),
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
			effort := xutil.FirstNonEmpty(
				xutil.LookupStringFromAny(value["reasoningEffort"]),
				xutil.LookupStringFromAny(value["reasoning_effort"]),
				xutil.LookupStringFromAny(value["id"]),
				xutil.LookupStringFromAny(value["value"]),
			)
			if effort == "" {
				continue
			}
			options = append(options, agentproto.ReasoningEffortOption{
				ReasoningEffort: effort,
				Description: xutil.FirstNonEmpty(
					xutil.LookupStringFromAny(value["description"]),
					xutil.LookupStringFromAny(value["label"]),
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
				ID: xutil.FirstNonEmpty(
					xutil.LookupStringFromAny(value["id"]),
					xutil.LookupStringFromAny(value["value"]),
				),
				Name: xutil.FirstNonEmpty(
					xutil.LookupStringFromAny(value["name"]),
					xutil.LookupStringFromAny(value["label"]),
				),
				Description: xutil.LookupStringFromAny(value["description"]),
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
		Model: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["model"]),
			xutil.LookupStringFromAny(item["targetModel"]),
			xutil.LookupStringFromAny(item["target_model"]),
		),
		UpgradeCopy: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["upgradeCopy"]),
			xutil.LookupStringFromAny(item["upgrade_copy"]),
		),
		ModelLink: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["modelLink"]),
			xutil.LookupStringFromAny(item["model_link"]),
		),
		MigrationMarkdown: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["migrationMarkdown"]),
			xutil.LookupStringFromAny(item["migration_markdown"]),
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
		return xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(value["message"]),
			xutil.LookupStringFromAny(value["title"]),
			xutil.LookupStringFromAny(value["description"]),
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
