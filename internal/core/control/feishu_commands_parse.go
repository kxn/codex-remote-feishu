package control

import "strings"

// ParseFeishuTextActionWithoutCatalog parses slash-style text into an action
// shape but intentionally strips catalog provenance. Runtime callers that need
// backend-aware command routing must follow with ResolveFeishuActionCatalog.
func ParseFeishuTextActionWithoutCatalog(text string) (Action, bool) {
	resolved, ok := ResolveFeishuTextCommand(CatalogContext{}, text)
	if !ok {
		return Action{}, false
	}
	return actionWithoutCatalogProvenance(resolved.Action), true
}

// ParseFeishuTextAction is a compatibility wrapper around
// ParseFeishuTextActionWithoutCatalog.
func ParseFeishuTextAction(text string) (Action, bool) {
	return ParseFeishuTextActionWithoutCatalog(text)
}

func ResolveFeishuTextCommand(ctx CatalogContext, text string) (ResolvedCommand, bool) {
	ctx = NormalizeCatalogContext(ctx)
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ResolvedCommand{}, false
	}
	fields := strings.Fields(trimmed)
	if len(fields) > 0 {
		first := strings.ToLower(fields[0])
		for _, spec := range feishuCommandSpecs {
			for _, prefix := range spec.textPrefixes {
				if first == prefix.alias {
					return resolvedFeishuCommandFromSpec(ctx, spec, Action{
						Kind:      prefix.kind,
						Text:      trimmed,
						CommandID: spec.definition.ID,
					}), true
				}
			}
		}
	}
	for _, spec := range feishuCommandSpecs {
		for _, match := range spec.textExact {
			if trimmed == match.alias {
				action := match.action
				if strings.TrimSpace(action.Text) == "" {
					action.Text = trimmed
				}
				action.CommandID = spec.definition.ID
				return resolvedFeishuCommandFromSpec(ctx, spec, action), true
			}
		}
	}
	return ResolvedCommand{}, false
}

// ParseFeishuMenuActionWithoutCatalog parses menu callback keys into an action
// shape but intentionally strips catalog provenance. Runtime callers that need
// backend-aware command routing must follow with ResolveFeishuActionCatalog.
func ParseFeishuMenuActionWithoutCatalog(eventKey string) (Action, bool) {
	resolved, ok := ResolveFeishuMenuCommand(CatalogContext{}, eventKey)
	if !ok {
		return Action{}, false
	}
	return actionWithoutCatalogProvenance(resolved.Action), true
}

// ParseFeishuMenuAction is a compatibility wrapper around
// ParseFeishuMenuActionWithoutCatalog.
func ParseFeishuMenuAction(eventKey string) (Action, bool) {
	return ParseFeishuMenuActionWithoutCatalog(eventKey)
}

func ResolveFeishuMenuCommand(ctx CatalogContext, eventKey string) (ResolvedCommand, bool) {
	ctx = NormalizeCatalogContext(ctx)
	trimmed := strings.TrimSpace(eventKey)
	if trimmed == "" {
		return ResolvedCommand{}, false
	}
	lower := strings.ToLower(trimmed)
	for _, spec := range feishuCommandSpecs {
		for _, dynamic := range spec.menuDynamic {
			if strings.HasPrefix(lower, dynamic.prefix) {
				argument, ok := dynamic.parseArgument(trimmed[len(dynamic.prefix):])
				if !ok {
					return ResolvedCommand{}, false
				}
				text := BuildFeishuActionText(dynamic.kind, argument)
				if strings.TrimSpace(text) == "" {
					return ResolvedCommand{}, false
				}
				return resolvedFeishuCommandFromSpec(ctx, spec, Action{
					Kind:      dynamic.kind,
					Text:      text,
					CommandID: spec.definition.ID,
				}), true
			}
		}
	}
	normalized := NormalizeFeishuMenuEventKey(trimmed)
	for _, spec := range feishuCommandSpecs {
		for _, match := range spec.menuExact {
			if normalized == NormalizeFeishuMenuEventKey(match.alias) {
				action := match.action
				action.CommandID = spec.definition.ID
				return resolvedFeishuCommandFromSpec(ctx, spec, action), true
			}
		}
	}
	return ResolvedCommand{}, false
}

func NormalizeFeishuMenuEventKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func actionWithoutCatalogProvenance(action Action) Action {
	action.CatalogFamilyID = ""
	action.CatalogVariantID = ""
	action.CatalogBackend = ""
	return action
}
