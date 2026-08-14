package agentproto

type ExplorationActionKind string

const (
	ExplorationActionRead   ExplorationActionKind = "read"
	ExplorationActionList   ExplorationActionKind = "list"
	ExplorationActionSearch ExplorationActionKind = "search"
)

type ExplorationAction struct {
	Kind      ExplorationActionKind `json:"kind,omitempty"`
	Items     []string              `json:"items,omitempty"`
	Summary   string                `json:"summary,omitempty"`
	Secondary string                `json:"secondary,omitempty"`
}

type ExplorationActions struct {
	Actions []ExplorationAction `json:"actions,omitempty"`
}
