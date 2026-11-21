package workmigration

import (
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// Terminal notifications remain observations, not quiescence. When they reveal
// native session IDs they strengthen the exact-instance history fence.
func (c *Coordinator) fenceLegacyEvents(tx *workstore.Tx, instance, kind string, raw json.RawMessage) error {
	var events []agentproto.Event
	switch kind {
	case "events":
		if err := json.Unmarshal(raw, &events); err != nil {
			return err
		}
	case "event":
		var event agentproto.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return err
		}
		events = append(events, event)
	default:
		return nil
	}
	backends, err := tx.LegacyObservedBackends(instance)
	if err != nil {
		return err
	}
	if len(backends) == 0 {
		return nil
	} // No backend attribution is invented.
	authority, err := tx.GetMigrationAuthority()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, event := range events {
		if event.ThreadID == "" || seen[event.ThreadID] {
			continue
		}
		seen[event.ThreadID] = true
		for _, backend := range backends {
			native := LegacyNative{Backend: backend, Namespace: string(backend), SessionID: event.ThreadID, InstanceID: instance, Facts: raw}
			key := "live-session:" + instance + ":" + string(backend) + ":" + event.ThreadID
			evidence, _ := json.Marshal(map[string]any{"native": native, "classification": "legacy_session_observed_unknown", "source": "exact_instance_native_events"})
			if err = putNativeFences(tx, authority.MigrationID, key, native, "legacy_native_requires_quiescence", evidence); err != nil {
				return err
			}
		}
	}
	return nil
}
