package work

import "time"

// ReviewResult preserves a native read-only review. It makes no business
// acceptance claim and cannot complete a task or approve a delivery.
type ReviewResult struct {
	AttemptID   string      `json:"attempt_id"`
	ContextID   string      `json:"context_id"`
	ScopeID     string      `json:"scope_id"`
	RunRef      RunRef      `json:"run_ref"`
	Spec        ReviewSpec  `json:"spec"`
	BodyRef     string      `json:"body_ref"`
	Terminal    SourceRef   `json:"terminal"`
	TextSources []SourceRef `json:"text_sources"`
	Delivered   bool        `json:"delivered"`
	Revision    int64       `json:"revision"`
	CreatedAt   time.Time   `json:"created_at"`
}
