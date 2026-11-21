package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) validateProjectHomeDirectory(v ProjectHome, old *ProjectHome, operation work.Operation) error {
	if v.DirectoryGeneration < 0 || v.DirectoryAppliedGeneration < 0 || v.DirectoryAppliedGeneration > v.DirectoryGeneration {
		return fmt.Errorf("invalid project directory generations")
	}
	if old != nil {
		if v.DirectoryGeneration < old.DirectoryGeneration || v.DirectoryGeneration > old.DirectoryGeneration+1 || v.DirectoryAppliedGeneration < old.DirectoryAppliedGeneration {
			return ErrConflict
		}
		if v.DirectoryGeneration == old.DirectoryGeneration && v.DirectorySelectionRef != old.DirectorySelectionRef {
			return fmt.Errorf("directory choice is immutable within its generation")
		}
	}
	if v.DirectoryGeneration == 0 {
		if v.DirectorySelectionRef != "" {
			return fmt.Errorf("directory choice requires a generation")
		}
		return nil
	}
	content, err := t.GetContent(v.ScopeID, v.DirectorySelectionRef)
	if err != nil {
		return err
	}
	var choice struct {
		Container   string         `json:"container"`
		Generation  int64          `json:"generation"`
		Source      work.SourceRef `json:"source"`
		PrincipalID string         `json:"principal_id"`
	}
	if err = json.Unmarshal(content.Data, &choice); err != nil {
		return err
	}
	if choice.Container == "" || choice.Generation != v.DirectoryGeneration || operation.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: choice.PrincipalID}) {
		return ErrScopeDenied
	}
	return t.validateInstructionSource(v.ScopeID, operation.Actor, choice.Source)
}
