package privatechat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type CodexReference struct {
	SurfaceID, Kind, Name string
	Admission             state.CodexAdmissionRef
}

// CodexReferences reads persisted owner checkpoints, including owners that
// have not been loaded by the current processor. Opaque recovery holds are
// searched recursively so retained pending bindings cannot lose revisions.
func (j *Journal) CodexReferences(ctx context.Context) ([]CodexReference, error) {
	rows, err := j.db.QueryContext(ctx, `SELECT data FROM private_checkpoint ORDER BY tenant,app_id,gateway,surface,generation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CodexReference
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var cp orchestrator.PrivateCheckpoint
		if err = json.Unmarshal(raw, &cp); err != nil {
			return nil, err
		}
		appendRef := func(kind, name string, ref *state.CodexAdmissionRef) {
			if normalized := state.NormalizeCodexAdmissionRef(ref); normalized != nil {
				out = append(out, CodexReference{cp.SurfaceID, kind, name, *normalized})
			}
		}
		appendRef("surface_actual", cp.SurfaceID, cp.CodexAdmissionRef)
		for _, item := range cp.QueueItems {
			if item != nil {
				kind := "queue_item"
				if item.ID == cp.ActiveQueueItemID {
					kind = "active_queue_item"
				}
				appendRef(kind, item.ID, item.CodexAdmissionRef)
			}
		}
		for _, hold := range cp.Holds {
			var value any
			if err = json.Unmarshal(hold.Frozen, &value); err != nil {
				return nil, err
			}
			var walk func(any)
			walk = func(value any) {
				switch node := value.(type) {
				case map[string]any:
					for k, v := range node {
						if strings.EqualFold(strings.ReplaceAll(k, "_", ""), "CodexAdmissionRef") {
							data, _ := json.Marshal(v)
							var ref state.CodexAdmissionRef
							if json.Unmarshal(data, &ref) == nil {
								appendRef("recovery_hold", hold.Kind, &ref)
							}
						}
						walk(v)
					}
				case []any:
					for _, v := range node {
						walk(v)
					}
				}
			}
			walk(value)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	effects, err := j.db.QueryContext(ctx, `SELECT e.surface,e.id,e.body FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE e.state<>'confirmed' OR a.effect_id IS NULL ORDER BY e.rowid`)
	if err != nil {
		return nil, err
	}
	defer effects.Close()
	for effects.Next() {
		var surface, id string
		var body []byte
		if err = effects.Scan(&surface, &id, &body); err != nil {
			return nil, err
		}
		event, e := DecodeEffectEvent(body)
		if e != nil {
			return nil, e
		}
		if event.DaemonCommand != nil {
			if ref := state.NormalizeCodexAdmissionRef(event.DaemonCommand.CodexAdmissionRef); ref != nil {
				out = append(out, CodexReference{SurfaceID: surface, Kind: "pending_effect", Name: id, Admission: *ref})
			}
		}
	}
	if err = effects.Err(); err != nil {
		return nil, err
	}
	if err = effects.Close(); err != nil {
		return nil, err
	}
	plans, err := j.db.QueryContext(ctx, `SELECT i.surface,r.plan FROM private_migration_resume r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE r.effect_id='' OR EXISTS (SELECT 1 FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE e.id=r.effect_id AND (e.state<>'confirmed' OR a.effect_id IS NULL))`)
	if err != nil {
		return nil, err
	}
	defer plans.Close()
	for plans.Next() {
		var surface string
		var raw []byte
		if err = plans.Scan(&surface, &raw); err != nil {
			return nil, err
		}
		var plan MigrationResumeIntent
		if err = json.Unmarshal(raw, &plan); err != nil {
			return nil, err
		}
		if strings.EqualFold(plan.Backend, "codex") && plan.ProfileID != "" {
			out = append(out, CodexReference{SurfaceID: surface, Kind: "migration_resume", Name: plan.WorkerID, Admission: state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: plan.ProfileID}}})
		}
	}
	return out, plans.Err()
}
