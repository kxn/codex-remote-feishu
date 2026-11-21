package workservice

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func lifecycleCommandDTOs() map[string]any {
	return map[string]any{
		"team.materials.select": TeamMaterialsSelectRequest{},
		"team.materials.share":  TeamMaterialsShareRequest{},
		"role.list":             KnowledgeListRequest{}, "role.inspect": RoleInspectRequest{}, "role.update-description": RoleDescriptionRequest{}, "team.list": KnowledgeListRequest{}, "team.inspect": TeamInspectRequest{}, "team.create": TeamCreateRequest{}, "team.update": TeamUpdateRequest{}, "team.member.add": TeamMemberRequest{}, "team.member.remove": TeamMemberRequest{},
		"project.inspect": ProjectInspectRequest{}, "project.pause": ProjectLifecycleRequest{}, "project.resume": ProjectLifecycleRequest{}, "project.archive": ProjectLifecycleRequest{}, "project.reopen": ProjectLifecycleRequest{}, "project.member.add": ProjectMemberRequest{}, "project.member.remove": ProjectMemberRequest{}, "project.leader.change": ProjectLeaderRequest{},
		"task.list": TaskListRequest{}, "task.inspect": TaskInspectRequest{},
		"task.start": TaskTransitionRequest{}, "task.wait": TaskTransitionRequest{}, "task.check": TaskTransitionRequest{}, "task.done": TaskTransitionRequest{}, "task.cancel": TaskTransitionRequest{}, "task.pause": TaskTransitionRequest{}, "task.resume": TaskTransitionRequest{},
		"task.evidence.submit": TaskEvidenceSubmitRequest{}, "task.evidence.list": TaskSubmissionListRequest{}, "task.evidence.adopt": TaskEvidenceAdoptRequest{}, "task.review": TaskEvidenceAdoptRequest{},
		"task.dependency.add": TaskDependencyRequest{}, "task.dependency.satisfy": TaskDependencyRequest{},
	}
}
func (s *Service) executeLifecycle(ctx context.Context, token string, req workcmd.DispatchRequest) (any, bool, error) {
	switch req.Name {
	case "team.materials.select":
		return executeDTO(ctx, token, req, s.TeamMaterialsSelect)
	case "team.materials.share":
		return executeDTO(ctx, token, req, s.TeamMaterialsShare)
	case "role.list":
		return executeDTO(ctx, token, req, s.RoleList)
	case "role.inspect":
		return executeDTO(ctx, token, req, s.RoleInspect)
	case "role.update-description":
		return executeDTO(ctx, token, req, s.RoleUpdateDescription)
	case "team.list":
		return executeDTO(ctx, token, req, s.TeamList)
	case "team.inspect":
		return executeDTO(ctx, token, req, s.TeamInspect)
	case "team.create":
		return executeDTO(ctx, token, req, s.TeamCreate)
	case "team.update":
		return executeDTO(ctx, token, req, s.TeamUpdate)
	case "team.member.add":
		return executeDTO(ctx, token, req, s.TeamMemberAdd)
	case "team.member.remove":
		return executeDTO(ctx, token, req, s.TeamMemberRemove)
	case "project.inspect":
		return executeDTO(ctx, token, req, s.ProjectInspect)
	case "project.pause":
		return executeDTO(ctx, token, req, s.ProjectPause)
	case "project.resume":
		return executeDTO(ctx, token, req, s.ProjectResume)
	case "project.archive":
		return executeDTO(ctx, token, req, s.ProjectArchive)
	case "project.reopen":
		return executeDTO(ctx, token, req, s.ProjectReopen)
	case "project.member.add":
		return executeDTO(ctx, token, req, s.ProjectMemberAdd)
	case "project.member.remove":
		return executeDTO(ctx, token, req, s.ProjectMemberRemove)
	case "project.leader.change":
		return executeDTO(ctx, token, req, s.ProjectLeaderChange)
	case "task.list":
		return executeDTO(ctx, token, req, s.TaskList)
	case "task.inspect":
		return executeDTO(ctx, token, req, s.TaskInspect)
	case "task.start":
		return executeDTO(ctx, token, req, s.TaskStart)
	case "task.wait":
		return executeDTO(ctx, token, req, s.TaskWait)
	case "task.check":
		return executeDTO(ctx, token, req, s.TaskCheck)
	case "task.done":
		return executeDTO(ctx, token, req, s.TaskDone)
	case "task.cancel":
		return executeDTO(ctx, token, req, s.TaskCancel)
	case "task.pause":
		return executeDTO(ctx, token, req, s.TaskPause)
	case "task.resume":
		return executeDTO(ctx, token, req, s.TaskResume)
	case "task.evidence.submit":
		return executeDTO(ctx, token, req, s.TaskEvidenceSubmit)
	case "task.evidence.list":
		return executeDTO(ctx, token, req, s.TaskEvidenceList)
	case "task.evidence.adopt":
		return executeDTO(ctx, token, req, s.TaskEvidenceAdopt)
	case "task.review":
		return executeDTO(ctx, token, req, s.TaskReview)
	case "task.dependency.add":
		return executeDTO(ctx, token, req, s.TaskDependencyAdd)
	case "task.dependency.satisfy":
		return executeDTO(ctx, token, req, s.TaskDependencySatisfy)
	default:
		return nil, false, nil
	}
}
