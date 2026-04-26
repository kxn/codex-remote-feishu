package control

import "strings"

func ParseFeishuTextAction(text string) (Action, bool) {
	resolved, ok := ResolveFeishuTextCommand(CatalogContext{}, text)
	if !ok {
		return Action{}, false
	}
	return resolved.Action, true
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
					return NormalizeResolvedCommand(ResolvedCommand{
						FamilyID:  spec.definition.ID,
						VariantID: defaultFeishuCommandDisplayVariantID(spec.definition.ID),
						Backend:   ctx.Backend,
						Action:    Action{Kind: prefix.kind, Text: trimmed, CommandID: spec.definition.ID},
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
				return NormalizeResolvedCommand(ResolvedCommand{
					FamilyID:  spec.definition.ID,
					VariantID: defaultFeishuCommandDisplayVariantID(spec.definition.ID),
					Backend:   ctx.Backend,
					Action:    action,
				}), true
			}
		}
	}
	return ResolvedCommand{}, false
}

func ParseFeishuMenuAction(eventKey string) (Action, bool) {
	resolved, ok := ResolveFeishuMenuCommand(CatalogContext{}, eventKey)
	if !ok {
		return Action{}, false
	}
	return resolved.Action, true
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
				text, ok := dynamic.build(trimmed[len(dynamic.prefix):])
				if !ok {
					return ResolvedCommand{}, false
				}
				return NormalizeResolvedCommand(ResolvedCommand{
					FamilyID:  spec.definition.ID,
					VariantID: defaultFeishuCommandDisplayVariantID(spec.definition.ID),
					Backend:   ctx.Backend,
					Action:    Action{Kind: dynamic.kind, Text: text, CommandID: spec.definition.ID},
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
				return NormalizeResolvedCommand(ResolvedCommand{
					FamilyID:  spec.definition.ID,
					VariantID: defaultFeishuCommandDisplayVariantID(spec.definition.ID),
					Backend:   ctx.Backend,
					Action:    action,
				}), true
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
