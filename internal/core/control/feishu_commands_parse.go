package control

import "strings"

// ParseFeishuTextActionWithoutCatalog parses slash-style text into a syntax-only
// action shape. Runtime callers that need backend-aware command routing must
// resolve catalog provenance later with the current surface context.
func ParseFeishuTextActionWithoutCatalog(text string) (Action, bool) {
	return parseFeishuTextAction(text)
}

func ResolveFeishuTextCommand(ctx CatalogContext, text string) (ResolvedCommand, bool) {
	ctx = NormalizeCatalogContext(ctx)
	action, ok := parseFeishuTextAction(text)
	if !ok {
		return ResolvedCommand{}, false
	}
	return resolvedCommandFromCommandID(ctx, action.CommandID, action)
}

// ParseFeishuMenuActionWithoutCatalog parses menu callback keys into a
// syntax-only action shape. Runtime callers that need backend-aware command
// routing must resolve catalog provenance later with the current surface
// context.
func ParseFeishuMenuActionWithoutCatalog(eventKey string) (Action, bool) {
	return parseFeishuMenuAction(eventKey)
}

func ResolveFeishuMenuCommand(ctx CatalogContext, eventKey string) (ResolvedCommand, bool) {
	ctx = NormalizeCatalogContext(ctx)
	action, ok := parseFeishuMenuAction(eventKey)
	if !ok {
		return ResolvedCommand{}, false
	}
	return resolvedCommandFromCommandID(ctx, action.CommandID, action)
}

func parseFeishuTextAction(text string) (Action, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Action{}, false
	}
	fields := strings.Fields(trimmed)
	if len(fields) > 0 {
		first := strings.ToLower(fields[0])
		for _, spec := range feishuCommandSpecs {
			for _, prefix := range spec.textPrefixes {
				if first == prefix.alias {
					return parsedFeishuCommandAction(spec, Action{
						Kind: prefix.kind,
						Text: trimmed,
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
				return parsedFeishuCommandAction(spec, action), true
			}
		}
	}
	return Action{}, false
}

func parseFeishuMenuAction(eventKey string) (Action, bool) {
	trimmed := strings.TrimSpace(eventKey)
	if trimmed == "" {
		return Action{}, false
	}
	lower := strings.ToLower(trimmed)
	for _, spec := range feishuCommandSpecs {
		for _, dynamic := range spec.menuDynamic {
			if strings.HasPrefix(lower, dynamic.prefix) {
				argument, ok := dynamic.parseArgument(trimmed[len(dynamic.prefix):])
				if !ok {
					return Action{}, false
				}
				text := BuildFeishuActionText(dynamic.kind, argument)
				if strings.TrimSpace(text) == "" {
					return Action{}, false
				}
				return parsedFeishuCommandAction(spec, Action{
					Kind: dynamic.kind,
					Text: text,
				}), true
			}
		}
	}
	normalized := NormalizeFeishuMenuEventKey(trimmed)
	for _, spec := range feishuCommandSpecs {
		for _, match := range spec.menuExact {
			if normalized == NormalizeFeishuMenuEventKey(match.alias) {
				action := match.action
				if strings.TrimSpace(action.Text) == "" {
					action.Text = strings.TrimSpace(spec.definition.CanonicalSlash)
				}
				return parsedFeishuCommandAction(spec, action), true
			}
		}
	}
	return Action{}, false
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

func parsedFeishuCommandAction(spec feishuCommandSpec, action Action) Action {
	action.CommandID = strings.TrimSpace(spec.definition.ID)
	return action
}
