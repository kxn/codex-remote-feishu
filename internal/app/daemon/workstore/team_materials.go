package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// SelectTeamMaterials is called only after authenticating an explicit human
// selection. Existing unknown writes remain immutable evidence, never replayed.
func (t *Tx) SelectTeamMaterials(teamID string, expected int64, scopeID string, selection work.TeamMaterialsSelection, binding work.RoleBotBinding, source work.SourceRef) (work.Team, error) {
	team, err := t.GetTeam(teamID)
	if err != nil {
		return team, err
	}
	if team.Revision != expected {
		return team, ErrConflict
	}
	if err = selection.Validate(); err != nil {
		return team, err
	}
	generation := int64(1)
	if team.Materials != nil {
		generation = team.Materials.Generation + 1
		// Once creation started, replacing an unresolved intent with another create
		// could duplicate the original. An explicit existing document is the exit.
		canCreate, e := t.TeamMaterialsCanSelectNew(team)
		if e != nil {
			return team, e
		}
		if selection.Mode != "existing" && !canCreate {
			return team, fmt.Errorf("团队已有资料建立记录；请选择现有首页继续使用，避免另建副本")
		}
	}
	team.Materials = &work.TeamMaterials{ScopeID: scopeID, Generation: generation, Selection: selection, Identity: binding, Source: source, State: "pending"}
	team.Revision++
	return team, t.PutTeam(team, expected)
}

func (t *Tx) TeamMaterialsCanSelectNew(team work.Team) (bool, error) {
	if team.Materials == nil {
		return true, nil
	}
	h := team.Materials
	if h.DocumentID != "" || h.FolderID != "" {
		return false, nil
	}
	ops, err := t.ListOperations(h.ScopeID)
	if err != nil {
		return false, err
	}
	for _, op := range ops {
		if op.TargetID != team.ID || op.TargetGeneration != h.Generation {
			continue
		}
		switch op.Kind {
		case "team.materials.folder", "team.materials.document", "team.materials.intro":
			if op.State != work.OperationPrepared {
				return false, nil
			}
		}
	}
	return true, nil
}
