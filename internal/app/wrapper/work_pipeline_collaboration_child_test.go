package wrapper

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type pipelineCollaborationCLI func(string, any, any) error

func pipelineCollaborate(env pipelineWorkEnvelope, call pipelineCollaborationCLI) (string, error) {
	var owner struct {
		Role string `json:"roleId"`
	}
	if err := json.Unmarshal([]byte(os.Getenv(config.WorkOwnerBindingEnv)), &owner); err != nil {
		return "", err
	}
	switch owner.Role {
	case "pipeline-role":
		for _, input := range env.Inputs {
			if input.Message == nil {
				continue
			}
			if input.Message.Source.Kind == work.ActorPrincipal && input.Message.Source.ID != "pipeline-role" {
				switch {
				case strings.Contains(input.Body, "Add CSV and Excel export"):
					if err := call.assign(input); err != nil {
						return "", err
					}
				case strings.Contains(input.Body, pipelineRevisedRequest):
					if err := call.revise(input); err != nil {
						return "", err
					}
				case strings.Contains(input.Body, pipelineDelegateReview):
					if err := call.delegate(input); err != nil {
						return "", err
					}
				}
			}
			if input.Message.Source == (work.Actor{Kind: work.ActorRole, ID: "pipeline-expert"}) && strings.HasPrefix(input.Body, "evidence:") {
				if err := call.review(input); err != nil {
					return "", err
				}
			}
		}
		return "complete", nil
	case "pipeline-expert":
		return call.engineer(env)
	case "pipeline-reviewer":
		for _, input := range env.Inputs {
			if env.Task != nil || input.RelatedTask == nil || !strings.HasPrefix(input.Body, "review:") || input.Message == nil ||
				input.Message.Source != (work.Actor{Kind: work.ActorRole, ID: "pipeline-expert"}) {
				return "", fmt.Errorf("reviewer's native turn did not receive the engineer work as a read-only association: %+v", input)
			}
			task := input.RelatedTask
			version := strconv.FormatInt(task.RequirementsRevision, 10)
			if err := call("message.send", workservice.SendMessageRequest{Key: "review-response-" + version,
				RecipientRoleID: task.OwnerRoleID, TaskID: task.ID, ConversationID: input.Message.ConversationID,
				Body: "checked:" + version}, nil); err != nil {
				return "", err
			}
		}
		return "complete", nil
	}
	return "", fmt.Errorf("unexpected collaboration role %q", owner.Role)
}

func (call pipelineCollaborationCLI) assign(input pipelineWorkInput) error {
	var current workservice.ContextView
	if err := call("context", struct{}{}, &current); err != nil {
		return err
	}
	var project workservice.ProjectView
	if err := call("project.inspect", workservice.ProjectInspectRequest{ProjectID: current.Scope.ProjectID}, &project); err != nil {
		return err
	}
	if project.Project.LeaderRoleID != current.Role.ID || len(project.Members) != 3 {
		return fmt.Errorf("leader could not discover the current project roster: %+v", project)
	}
	for _, target := range []string{"pipeline-expert", "pipeline-reviewer"} {
		var role workservice.RoleDescriptionView
		if err := call("role.inspect", workservice.RoleInspectRequest{RoleID: target}, &role); err != nil {
			return err
		}
		if role.EffectiveDescription == "" {
			return fmt.Errorf("role %s lacks a discoverable duty", target)
		}
	}
	requirement := exportRequirement(current.Scope.ID, current.Role.ID, input)
	var task workservice.Result
	if err := call("task.assign", workservice.AssignTaskRequest{Key: "assign-export", Title: "CSV and Excel export",
		OwnerRoleID: "pipeline-expert", Requirements: []work.TaskRequirementRef{requirement}}, &task); err != nil {
		return err
	}
	return call("message.publish", workservice.PublishMessageRequest{Key: "export-topic", TaskID: task.ObjectID, Body: pipelineTopicSummary}, nil)
}

func exportRequirement(scopeID, roleID string, input pipelineWorkInput) work.TaskRequirementRef {
	message := input.Message
	return work.TaskRequirementRef{
		ID: "export-scope", ScopeID: scopeID, AdoptedBy: roleID,
		Source:           work.SourceRef{Kind: "message", ID: message.ID, Version: strconv.FormatInt(message.Revision, 10), EvidenceRef: message.BodyRef},
		RequiredEvidence: []string{"paired_check"},
	}
}

func (call pipelineCollaborationCLI) revise(input pipelineWorkInput) error {
	task := input.RelatedTask
	if task == nil || task.RequirementsRevision != 1 {
		return fmt.Errorf("leader received an unbound revision: %+v", input)
	}
	var agreement workservice.AgreementApplyResult
	if err := call("agreement.apply", workservice.AgreementApplyRequest{Key: "request-scope-review", ID: "export-participation",
		SourceMessageID: input.Message.ID, SubjectKind: work.AgreementItem, SubjectID: task.ID, DecisionOwner: input.Message.Source,
		ParticipationPoints: []string{"Scope review"}, Reporting: work.ReportingMilestones, Validity: work.AgreementPersistent,
		TaskImpacts: []workservice.AgreementTaskImpact{{TaskID: task.ID, TaskRevision: task.Revision, Point: "Scope review",
			Stage: "before_point", Facts: "CSV and Excel were previously checked. The CSV-only revision needs a new check.",
			Choices: []work.DecisionChoice{{Value: "continue", Label: "Continue with CSV"}, {Value: "revise", Label: "Revise scope"}}}}}, &agreement); err != nil {
		return err
	}
	if len(agreement.TaskOutcomes) != 1 || agreement.TaskOutcomes[0].DecisionRequestID == "" {
		return fmt.Errorf("human review did not establish a decision and gate: %+v", agreement)
	}
	requirement := exportRequirement(task.ScopeID, "pipeline-role", input)
	var revised workservice.Result
	if err := call("task.revise", workservice.ReviseTaskRequest{Key: "revise-export-csv", TaskID: task.ID,
		ExpectedRevision: task.Revision, Title: "CSV only export", OwnerRoleID: task.OwnerRoleID,
		Requirements: []work.TaskRequirementRef{requirement}}, &revised); err != nil {
		return err
	}
	var submissions workservice.KnowledgePage[workservice.TaskSubmissionView]
	if err := call("task.evidence.list", workservice.TaskSubmissionListRequest{TaskID: task.ID}, &submissions); err != nil {
		return err
	}
	if len(submissions.Items) != 1 {
		return fmt.Errorf("could not inspect the old evidence: %+v", submissions)
	}
	if err := call("task.review", workservice.TaskEvidenceAdoptRequest{Key: "stale-evidence",
		TaskID: task.ID, ExpectedRevision: revised.Revision, SubmissionID: submissions.Items[0].ID}, nil); err == nil {
		return fmt.Errorf("leader adopted old evidence as validation for the revised requirements")
	}
	return nil
}

func (call pipelineCollaborationCLI) delegate(input pipelineWorkInput) error {
	if input.RelatedTask == nil || input.RelatedTask.RequirementsRevision != 2 {
		return fmt.Errorf("participation update lost the revised work association: %+v", input)
	}
	var agreement workservice.AgreementApplyResult
	if err := call("agreement.apply", workservice.AgreementApplyRequest{Key: "delegate-scope-review", ID: "export-participation",
		ExpectedRevision: 1, SourceMessageID: input.Message.ID, SubjectKind: work.AgreementItem, SubjectID: input.RelatedTask.ID,
		DecisionOwner: work.Actor{Kind: work.ActorRole, ID: "pipeline-role"}, ParticipationPoints: []string{"Scope review"},
		Reporting: work.ReportingActionRequired, Validity: work.AgreementPersistent}, &agreement); err != nil {
		return err
	}
	if len(agreement.MigratedRequestIDs) != 1 {
		return fmt.Errorf("delegation left work waiting on the human: %+v", agreement)
	}
	var requests workservice.KnowledgePage[work.DecisionRequest]
	if err := call("decision.list", struct{}{}, &requests); err != nil {
		return err
	}
	for _, request := range requests.Items {
		if request.ID == agreement.MigratedRequestIDs[0] {
			return call("decision.respond", workservice.DecisionRespondRequest{
				Key: "lead-approved-csv", RequestID: request.ID, ExpectedRevision: request.Revision,
				RequestGeneration: request.RequestGeneration, AgreementRevision: request.AgreementRevision, Choice: "continue"}, nil)
		}
	}
	return fmt.Errorf("transferred choice is absent from current work: %+v", requests)
}

func (call pipelineCollaborationCLI) engineer(env pipelineWorkEnvelope) (string, error) {
	if env.Task == nil || len(env.Task.Requirements) != 1 {
		return "", fmt.Errorf("engineer did not get its frozen requirements: %+v", env.Task)
	}
	var observed workservice.TaskView
	if err := call("task.inspect", workservice.TaskInspectRequest{TaskID: env.Task.ID}, &observed); err != nil {
		return "", err
	}
	task := observed.Task
	version := strconv.FormatInt(task.RequirementsRevision, 10)
	for _, input := range env.Inputs {
		if input.Message == nil || input.Message.Source != (work.Actor{Kind: work.ActorRole, ID: "pipeline-reviewer"}) || input.Body != "checked:"+version {
			continue
		}
		var checked workservice.Result
		if err := call("task.check", workservice.TaskTransitionRequest{Key: "check-export-" + version, TaskID: task.ID, ExpectedRevision: task.Revision}, &checked); err != nil {
			return "", err
		}
		var evidence workservice.Result
		requirement := env.Task.Requirements[0]
		if err := call("task.evidence.submit", workservice.TaskEvidenceSubmitRequest{Key: "evidence-export-" + version,
			TaskID: task.ID, ExpectedRevision: checked.Revision, RequirementsRevision: env.Task.RequirementsRevision,
			AssignmentGeneration: env.Task.AssignmentGeneration, RequirementID: requirement.ID, RequirementVersion: requirement.Source.Version,
			Kind: "paired_check", Source: work.SourceRef{Kind: "message", ID: input.Message.ID, Version: version, EvidenceRef: input.Message.BodyRef}}, &evidence); err != nil {
			return "", err
		}
		return "complete", call("message.send", workservice.SendMessageRequest{Key: "report-export-" + version,
			RecipientRoleID: "pipeline-role", ConversationID: input.Message.ConversationID, Body: "evidence:" + evidence.ObjectID}, nil)
	}
	var started workservice.Result
	if err := call("task.start", workservice.TaskTransitionRequest{Key: "start-export-" + version, TaskID: task.ID, ExpectedRevision: task.Revision}, &started); err != nil {
		return "", err
	}
	if err := call("task.wait", workservice.TaskTransitionRequest{Key: "wait-export-" + version, TaskID: task.ID, ExpectedRevision: started.Revision}, nil); err != nil {
		return "", err
	}
	return "unfinished", call("message.send", workservice.SendMessageRequest{Key: "ask-review-" + version,
		RecipientRoleID: "pipeline-reviewer", ConversationID: "export:" + task.ID, Body: "review:" + version}, nil)
}

func (call pipelineCollaborationCLI) review(input pipelineWorkInput) error {
	task := input.RelatedTask
	if task == nil || task.State != work.TaskChecking {
		return fmt.Errorf("leader got evidence without the expert's related current task: %+v", input)
	}
	revision := strconv.FormatInt(task.RequirementsRevision, 10)
	if err := call("task.review", workservice.TaskEvidenceAdoptRequest{Key: "adopt-evidence-" + revision, TaskID: task.ID,
		ExpectedRevision: task.Revision, SubmissionID: strings.TrimPrefix(input.Body, "evidence:")}, nil); err != nil {
		return err
	}
	if task.RequirementsRevision == 1 {
		return nil
	}
	if err := call("task.done", workservice.TaskTransitionRequest{Key: "finish-export", TaskID: task.ID, ExpectedRevision: task.Revision}, nil); err != nil {
		return err
	}
	return call("message.publish", workservice.PublishMessageRequest{Key: "deliver-export", TaskID: task.ID, Body: pipelineDelivery}, nil)
}
