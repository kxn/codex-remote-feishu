package wrapper

// OpenCode V1 and V2 use different permission names. Both overlays deny every
// capability except file inspection, including MCP and delegated agents. A CLI
// rejecting the other schema fails initialization rather than running writable.
func enforceOpenCodeReviewPolicy(overlay map[string]any, agents map[string]any) {
	legacy := func() map[string]any {
		return map[string]any{"*": "deny", "read": "allow", "glob": "allow", "grep": "allow", "list": "allow"}
	}
	rules := func() []any {
		return []any{
			map[string]any{"action": "*", "resource": "*", "effect": "deny"},
			map[string]any{"action": "read", "resource": "*", "effect": "allow"},
			map[string]any{"action": "glob", "resource": "*", "effect": "allow"},
			map[string]any{"action": "grep", "resource": "*", "effect": "allow"},
		}
	}
	stamp := func(agent map[string]any) {
		agent["permission"] = legacy()
		agent["permissions"] = rules()
		agent["tools"] = map[string]any{"*": false, "read": true, "glob": true, "grep": true, "list": true}
	}
	for _, name := range []string{"build", "plan", "general", "explore", "title", "summary", "compaction"} {
		if agents[name] == nil {
			agents[name] = map[string]any{}
		}
	}
	for _, raw := range agents {
		if agent, ok := raw.(map[string]any); ok {
			stamp(agent)
		}
	}
	modern, _ := overlay["agents"].(map[string]any)
	if modern == nil {
		modern = map[string]any{}
	}
	for name, raw := range agents {
		if modern[name] == nil {
			modern[name] = raw
		}
	}
	for _, raw := range modern {
		if agent, ok := raw.(map[string]any); ok {
			stamp(agent)
		}
	}
	overlay["agents"] = modern
	overlay["permission"] = legacy()
	overlay["permissions"] = rules()
	overlay["mcp"] = map[string]any{}
	// Existing explicit plugin code runs outside native tool permission checks.
	// Review launches therefore load no configured plugin or shell command hook.
	overlay["plugin"] = []any{}
	overlay["plugins"] = []any{}
}
