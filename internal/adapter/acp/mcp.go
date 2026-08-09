package acp

import "strings"

type MCPServer struct {
	Name    string
	Type    string
	URL     string
	Headers []MCPNameValue
	Command string
	Args    []string
	Env     []MCPNameValue
}

type MCPNameValue struct {
	Name  string
	Value string
}

func (t *Translator) SetMCPServers(servers []MCPServer) {
	t.mcpServers = cloneMCPServers(servers)
}

func (t *Translator) mcpServersParam() []any {
	if len(t.mcpServers) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(t.mcpServers))
	for _, server := range t.mcpServers {
		name := strings.TrimSpace(server.Name)
		if name == "" {
			continue
		}
		serverType := strings.ToLower(strings.TrimSpace(server.Type))
		switch serverType {
		case "http", "sse":
			url := strings.TrimSpace(server.URL)
			if url == "" {
				continue
			}
			out = append(out, map[string]any{
				"type":    serverType,
				"name":    name,
				"url":     url,
				"headers": nameValuesParam(server.Headers),
			})
		case "":
			command := strings.TrimSpace(server.Command)
			if command == "" {
				continue
			}
			out = append(out, map[string]any{
				"name":    name,
				"command": command,
				"args":    stringsParam(server.Args),
				"env":     nameValuesParam(server.Env),
			})
		}
	}
	if len(out) == 0 {
		return []any{}
	}
	return out
}

func cloneMCPServers(servers []MCPServer) []MCPServer {
	if len(servers) == 0 {
		return nil
	}
	cloned := make([]MCPServer, 0, len(servers))
	for _, server := range servers {
		server.Headers = cloneMCPNameValues(server.Headers)
		server.Args = append([]string{}, server.Args...)
		server.Env = cloneMCPNameValues(server.Env)
		cloned = append(cloned, server)
	}
	return cloned
}

func cloneMCPNameValues(values []MCPNameValue) []MCPNameValue {
	if len(values) == 0 {
		return nil
	}
	return append([]MCPNameValue{}, values...)
}

func nameValuesParam(values []MCPNameValue) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value.Name)
		if name == "" {
			continue
		}
		out = append(out, map[string]any{
			"name":  name,
			"value": strings.TrimSpace(value.Value),
		})
	}
	return out
}

func stringsParam(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, strings.TrimSpace(value))
	}
	return out
}
