package agentproto

const RuntimeMCPProtocolVersion = 1
const CommandRuntimeMCPRead CommandKind = "runtime.mcp.read"
const EventRuntimeMCPResult EventKind = "runtime.mcp.result"

type RuntimeMCPQuery struct {
	Version int    `json:"version"`
	Mode    string `json:"mode"` // status or configuration; configuration is a safe summary
	Cursor  string `json:"cursor,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type RuntimeMCPServer struct {
	Name           string `json:"name"`
	Enabled        *bool  `json:"enabled,omitempty"`
	Transport      string `json:"transport,omitempty"`
	Origin         string `json:"origin,omitempty"` // scheme and host only, never credentials/path/query
	Status         string `json:"status,omitempty"`
	Authentication string `json:"authentication,omitempty"`
	ToolCount      int    `json:"toolCount,omitempty"`
}

type RuntimeMCPResult struct {
	Version    int                `json:"version"`
	Mode       string             `json:"mode"`
	Supported  bool               `json:"supported"`
	Servers    []RuntimeMCPServer `json:"servers"`
	NextCursor string             `json:"nextCursor,omitempty"`
	Problem    *ErrorInfo         `json:"problem,omitempty"`
}
