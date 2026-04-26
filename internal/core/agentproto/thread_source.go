package agentproto

type ThreadSourceKind string

const (
	ThreadSourceKindUser                ThreadSourceKind = "user"
	ThreadSourceKindAppServer           ThreadSourceKind = "app_server"
	ThreadSourceKindCustom              ThreadSourceKind = "custom"
	ThreadSourceKindReview              ThreadSourceKind = "review"
	ThreadSourceKindCompact             ThreadSourceKind = "compact"
	ThreadSourceKindThreadSpawn         ThreadSourceKind = "thread_spawn"
	ThreadSourceKindMemoryConsolidation ThreadSourceKind = "memory_consolidation"
	ThreadSourceKindSubAgentOther       ThreadSourceKind = "subagent_other"
	ThreadSourceKindUnknown             ThreadSourceKind = "unknown"
)

type ThreadSourceRecord struct {
	Kind           ThreadSourceKind `json:"kind,omitempty"`
	Name           string           `json:"name,omitempty"`
	ParentThreadID string           `json:"parentThreadId,omitempty"`
}

func CloneThreadSourceRecord(source *ThreadSourceRecord) *ThreadSourceRecord {
	if source == nil {
		return nil
	}
	cloned := *source
	return &cloned
}

func (s *ThreadSourceRecord) IsReview() bool {
	return s != nil && s.Kind == ThreadSourceKindReview
}
