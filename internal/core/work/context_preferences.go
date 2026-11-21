package work

import (
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// ContextPreferences overrides only one durable conversation context. Nil
// fields inherit runtime defaults; these values never mutate a Bot's settings.
type ContextPreferences struct {
	ContextID  string    `json:"context_id"`
	ScopeID    string    `json:"scope_id"`
	AccessMode *string   `json:"access_mode,omitempty"`
	PlanMode   *string   `json:"plan_mode,omitempty"`
	Verbose    *string   `json:"verbose,omitempty"`
	Source     SourceRef `json:"source"`
	UpdatedBy  Actor     `json:"updated_by"`
	Revision   int64     `json:"revision"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (v ContextPreferences) Validate() error {
	if err := required(v.ContextID, v.ScopeID); err != nil {
		return err
	}
	if err := positive("context preferences revision", v.Revision); err != nil {
		return err
	}
	if v.UpdatedBy.Kind != ActorPrincipal {
		return fmt.Errorf("context preferences require real human source")
	}
	if err := v.UpdatedBy.Validate(); err != nil {
		return err
	}
	if v.AccessMode != nil && (*v.AccessMode == "" || agentproto.NormalizeAccessMode(*v.AccessMode) != *v.AccessMode) {
		return fmt.Errorf("invalid canonical access mode")
	}
	if v.PlanMode != nil && *v.PlanMode != "on" && *v.PlanMode != "off" {
		return fmt.Errorf("plan mode must be on or off")
	}
	if v.Verbose != nil && !oneOf(*v.Verbose, "quiet", "normal", "verbose", "chatty") {
		return fmt.Errorf("invalid context verbosity")
	}
	return v.Source.Validate()
}
