package workservice

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func applyProjectTransition(tx *workstore.Tx, project work.Project, kind string) (work.Project, error) {
	var err error

	switch kind {
	case "project.pause":
		if project.State != work.ProjectActive && project.State != work.ProjectDegraded && project.State != work.ProjectPausing {
			return project, fmt.Errorf("project is not active or pausing")
		}
		project.State = work.ProjectPausing
		if err = projectGate(tx, project, true); err != nil {
			return project, err
		}
	case "project.resume":
		if project.State != work.ProjectPaused {
			return project, fmt.Errorf("project must be fully paused before resume")
		}
		project.State = work.ProjectDegraded
		if err = projectGate(tx, project, false); err != nil {
			return project, err
		}
	case "project.archive":
		if project.State == work.ProjectArchived {
			return project, fmt.Errorf("project already archived")
		}
		project.State = work.ProjectArchiving
		if err = projectGate(tx, project, true); err != nil {
			return project, err
		}
	case "project.reopen":
		if project.State != work.ProjectArchived {
			return project, fmt.Errorf("only an archived project can reopen")
		}
		project.State = work.ProjectPaused
		if err = projectGate(tx, project, true); err != nil {
			return project, err
		}
	}
	old := project.Revision
	project.Revision++
	if err = tx.PutProject(project, old); err != nil {
		return project, err
	}
	return project, nil
}
