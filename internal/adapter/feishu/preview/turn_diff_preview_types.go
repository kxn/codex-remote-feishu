package preview

import "time"

const (
	turnDiffPreviewArtifactKind = "turn_diff_snapshot"
	turnDiffPreviewRendererKind = "turn_diff"
	turnDiffPreviewSchemaV1     = 1
)

type turnDiffPreviewArtifact struct {
	SchemaVersion  int                   `json:"schemaVersion"`
	ThreadID       string                `json:"threadID,omitempty"`
	TurnID         string                `json:"turnID,omitempty"`
	GeneratedAt    time.Time             `json:"generatedAt,omitempty"`
	RawUnifiedDiff string                `json:"rawUnifiedDiff,omitempty"`
	Files          []turnDiffPreviewFile `json:"files,omitempty"`
}

type turnDiffPreviewFile struct {
	Key         string                `json:"key,omitempty"`
	Name        string                `json:"name,omitempty"`
	OldPath     string                `json:"oldPath,omitempty"`
	NewPath     string                `json:"newPath,omitempty"`
	ChangeKind  string                `json:"changeKind,omitempty"`
	Binary      bool                  `json:"binary,omitempty"`
	ParseStatus string                `json:"parseStatus,omitempty"`
	RawPatch    string                `json:"rawPatch,omitempty"`
	BeforeText  string                `json:"beforeText,omitempty"`
	AfterText   string                `json:"afterText,omitempty"`
	Stats       turnDiffPreviewStats  `json:"stats"`
	Lines       []turnDiffPreviewLine `json:"lines,omitempty"`
	Hunks       []turnDiffPreviewHunk `json:"hunks,omitempty"`
}

type turnDiffPreviewStats struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

type turnDiffPreviewLine struct {
	Kind string `json:"kind"`
	Old  string `json:"old"`
	Now  string `json:"now"`
	Text string `json:"text"`
}

type turnDiffPreviewHunk struct {
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
}

type turnDiffParsedFile struct {
	Index       int
	OldPath     string
	NewPath     string
	ChangeKind  string
	Binary      bool
	RawLines    []string
	HeaderLines []string
	Hunks       []turnDiffParsedHunk
}

type turnDiffParsedHunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	RawLine  string
	Lines    []turnDiffParsedLine
}

type turnDiffParsedLine struct {
	Kind byte
	Text string
}
