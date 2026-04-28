package control

import "strings"

type feishuCommandActionRoute struct {
	kind           ActionKind
	canonicalSlash string
}

func feishuCommandSpecByID(commandID string) (feishuCommandSpec, bool) {
	commandID = strings.TrimSpace(commandID)
	for _, spec := range feishuCommandSpecs {
		if spec.definition.ID == commandID {
			return spec, true
		}
	}
	return feishuCommandSpec{}, false
}

func feishuCommandPrimaryActionKind(spec feishuCommandSpec) (ActionKind, bool) {
	seen := make(map[ActionKind]struct{}, 4)
	var primary ActionKind
	record := func(kind ActionKind) bool {
		if strings.TrimSpace(string(kind)) == "" {
			return true
		}
		if _, ok := seen[kind]; ok {
			return true
		}
		if len(seen) > 0 {
			return false
		}
		seen[kind] = struct{}{}
		primary = kind
		return true
	}

	for _, match := range spec.textExact {
		if !record(match.action.Kind) {
			return "", false
		}
	}
	for _, match := range spec.textPrefixes {
		if !record(match.kind) {
			return "", false
		}
	}
	for _, match := range spec.menuExact {
		if !record(match.action.Kind) {
			return "", false
		}
	}
	for _, match := range spec.menuDynamic {
		if !record(match.kind) {
			return "", false
		}
	}
	if len(seen) == 0 {
		return "", false
	}
	return primary, true
}

func feishuCommandPrimaryActionRoute(spec feishuCommandSpec) (feishuCommandActionRoute, bool) {
	kind, ok := feishuCommandPrimaryActionKind(spec)
	if !ok {
		return feishuCommandActionRoute{}, false
	}
	canonicalSlash := strings.TrimSpace(spec.definition.CanonicalSlash)
	if canonicalSlash == "" {
		return feishuCommandActionRoute{}, false
	}
	return feishuCommandActionRoute{
		kind:           kind,
		canonicalSlash: canonicalSlash,
	}, true
}

func feishuCommandActionRouteByKind(kind ActionKind) (string, feishuCommandActionRoute, bool) {
	kind = ActionKind(strings.TrimSpace(string(kind)))
	if kind == "" {
		return "", feishuCommandActionRoute{}, false
	}
	for _, spec := range feishuCommandSpecs {
		if route, ok := feishuCommandPrimaryActionRoute(spec); ok && route.kind == kind {
			return spec.definition.ID, route, true
		}
		for _, route := range spec.extraActionRoutes {
			if route.kind != kind {
				continue
			}
			current := route
			if strings.TrimSpace(current.canonicalSlash) == "" {
				current.canonicalSlash = strings.TrimSpace(spec.definition.CanonicalSlash)
			}
			return spec.definition.ID, current, true
		}
	}
	return "", feishuCommandActionRoute{}, false
}

func resolvedFeishuCommandFromSpec(ctx CatalogContext, spec feishuCommandSpec, action Action) ResolvedCommand {
	commandID := strings.TrimSpace(spec.definition.ID)
	return NormalizeResolvedCommand(ResolvedCommand{
		FamilyID:  commandID,
		VariantID: feishuCommandVariantIDForContext(commandID, ctx),
		Backend:   ctx.Backend,
		Action:    action,
	})
}
