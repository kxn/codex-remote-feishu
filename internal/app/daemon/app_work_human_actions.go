package daemon

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"log"
)

// The SDK has already persisted the interaction. Reserve only a bounded local
// execution slot; a crash leaves the durable action available for reconciliation.
func (r *teamRuntime) queueHumanAction(interaction workstore.HumanInteraction, handle func(context.Context, workstore.HumanInteraction) error) bool {
	key := interaction.Envelope.Tenant + "\x00" + interaction.Envelope.ChatID
	r.humanActionsMu.Lock()
	if r.runContext == nil || r.runContext.Err() != nil || r.humanActionsPending[key] || len(r.humanActionsPending) >= 4 {
		r.humanActionsMu.Unlock()
		return false
	}
	if r.humanActionsPending == nil {
		r.humanActionsPending = make(map[string]bool)
	}
	r.humanActionsPending[key] = true
	r.workers.Add(1)
	r.humanActionsMu.Unlock()
	go func() {
		defer r.workers.Done()
		defer func() { r.humanActionsMu.Lock(); delete(r.humanActionsPending, key); r.humanActionsMu.Unlock() }()
		if err := handle(r.runContext, interaction); err != nil {
			log.Printf("team human action %s: %v", interaction.ID, err)
		}
	}()
	return true
}
