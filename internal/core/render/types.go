package render

type BlockKind string

const (
	BlockAssistantMarkdown BlockKind = "assistant_markdown"
	BlockAssistantCode     BlockKind = "assistant_code"
	BlockStatus            BlockKind = "status"
	BlockError             BlockKind = "error"
)

type Block struct {
	ID                    string    `json:"id"`
	SurfaceSessionID      string    `json:"surfaceSessionId,omitempty"`
	InstanceID            string    `json:"instanceId,omitempty"`
	ThreadID              string    `json:"threadId,omitempty"`
	ThreadTitle           string    `json:"threadTitle,omitempty"`
	TurnID                string    `json:"turnId,omitempty"`
	ItemID                string    `json:"itemId,omitempty"`
	Kind                  BlockKind `json:"kind"`
	Text                  string    `json:"text"`
	Language              string    `json:"language,omitempty"`
	ThemeKey              string    `json:"themeKey,omitempty"`
	TemporarySessionLabel string    `json:"temporarySessionLabel,omitempty"`
	Final                 bool      `json:"final,omitempty"`
}
