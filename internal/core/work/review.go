package work

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type ReviewSpec struct {
	SourceContextID    string                  `json:"source_context_id"`
	ResourceScopeID    string                  `json:"resource_scope_id"`
	LocationID         string                  `json:"location_id"`
	LocationGeneration int64                   `json:"location_generation"`
	Target             agentproto.ReviewTarget `json:"target"`
	Source             SourceRef               `json:"source"`
}

func (v ReviewSpec) Validate() error {
	if err := required(v.SourceContextID, v.ResourceScopeID, v.LocationID); err != nil {
		return err
	}
	if v.LocationGeneration < 1 || !v.Target.Valid() {
		return fmt.Errorf("review requires frozen location and explicit valid target")
	}
	return v.Source.Validate()
}
