package privatechat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type OpenCodeReference struct{ SurfaceID, Kind, Name, ProfileID string }

func (j *Journal) OpenCodeReferences(ctx context.Context) ([]OpenCodeReference, error) {
	rows, err := j.db.QueryContext(ctx, `SELECT data FROM private_checkpoint ORDER BY tenant,app_id,gateway,surface,generation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenCodeReference
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var cp orchestrator.PrivateCheckpoint
		if err = json.Unmarshal(raw, &cp); err != nil {
			return nil, err
		}
		add := func(kind, name, id string) {
			if id != "" {
				out = append(out, OpenCodeReference{cp.SurfaceID, kind, name, id})
			}
		}
		if cp.Backend == "opencode" {
			add("surface_desired", cp.SurfaceID, cp.OpenCodeProfileID)
		}
		if ref := state.NormalizeOpenCodeAdmissionRef(cp.OpenCodeAdmissionRef); ref != nil {
			add("surface_actual", cp.SurfaceID, ref.ProfileRef.ID)
		}
		for _, item := range cp.QueueItems {
			if item != nil {
				if ref := state.NormalizeOpenCodeAdmissionRef(item.OpenCodeAdmissionRef); ref != nil {
					add("queue_item", item.ID, ref.ProfileRef.ID)
				}
			}
		}
		for _, hold := range cp.Holds {
			var value any
			if err = json.Unmarshal(hold.Frozen, &value); err != nil {
				return nil, err
			}
			var walk func(any)
			walk = func(v any) {
				switch n := v.(type) {
				case map[string]any:
					for k, x := range n {
						if strings.EqualFold(strings.ReplaceAll(k, "_", ""), "OpenCodeAdmissionRef") {
							b, _ := json.Marshal(x)
							var ref state.OpenCodeAdmissionRef
							if json.Unmarshal(b, &ref) == nil {
								if normalized := state.NormalizeOpenCodeAdmissionRef(&ref); normalized != nil {
									add("recovery_hold", hold.Kind, normalized.ProfileRef.ID)
								}
							}
						}
						walk(x)
					}
				case []any:
					for _, x := range n {
						walk(x)
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
		var raw []byte
		if err = effects.Scan(&surface, &id, &raw); err != nil {
			return nil, err
		}
		event, e := DecodeEffectEvent(raw)
		if e != nil {
			return nil, e
		}
		if event.DaemonCommand != nil {
			if ref := state.NormalizeOpenCodeAdmissionRef(event.DaemonCommand.OpenCodeAdmissionRef); ref != nil {
				out = append(out, OpenCodeReference{surface, "pending_effect", id, ref.ProfileRef.ID})
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
		if strings.EqualFold(plan.Backend, "opencode") && plan.ProfileID != "" {
			out = append(out, OpenCodeReference{surface, "migration_resume", plan.WorkerID, plan.ProfileID})
		}
	}
	return out, plans.Err()
}
