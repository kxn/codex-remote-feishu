package wrapper

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestSDKGroupTaskDependencyWakesCoordinatorAndWaitingParent(t *testing.T) {
	t.Setenv("FSC_NATIVE_PIPELINE_DEPENDENCY", "1")
	runSDKNativePipeline(t, buildPipelineTeamCLI(t), work.BackendCodex, 3)
}

func pipelineDependency(env pipelineWorkEnvelope, call pipelineCollaborationCLI) (string, error) {
	if env.Task == nil {
		for _, input := range env.Inputs {
			if input.Message == nil {
				continue
			}
			switch input.Message.Source.Kind {
			case work.ActorPrincipal:
				err := call("task.assign", workservice.AssignTaskRequest{Key: "parent", Title: "Deliver the research",
					OwnerRoleID: "pipeline-role"}, nil)
				return "complete", err
			case work.ActorSystem:
				return "complete", call.unblockParent(input)
			}
		}
		return "", fmt.Errorf("coordination run lacked original request or dependency message")
	}
	task := env.Task
	var current workservice.TaskView
	if err := call("task.inspect", workservice.TaskInspectRequest{TaskID: task.ID}, &current); err != nil {
		return "", err
	}
	if strings.HasPrefix(env.Inputs[0].EventID, "dependency-cleared:") {
		if current.Task.State != work.TaskWaiting || len(current.Dependencies) != 1 || current.Dependencies[0].SatisfiedBy == nil {
			return "", fmt.Errorf("parent resumed without explicit satisfaction: %+v", current)
		}
		if err := call("task.done", workservice.TaskTransitionRequest{Key: "parent-done", TaskID: task.ID,
			ExpectedRevision: current.Task.Revision}, nil); err != nil {
			return "", err
		}
		if err := call("message.send", workservice.SendMessageRequest{Key: "already-closed", RecipientRoleID: "pipeline-expert",
			ConversationID: "dependency", Body: "This completed run must not start unrelated work."}, nil); err == nil {
			return "", fmt.Errorf("completed task run could still perform other business mutations")
		}
		return "complete", call("message.publish", workservice.PublishMessageRequest{Key: "deliver-dependency",
			TaskID: task.ID, Body: "The specialist's result was verified and incorporated."}, nil)
	}
	var started workservice.Result
	if err := call("task.start", workservice.TaskTransitionRequest{Key: "start-" + task.ID, TaskID: task.ID,
		ExpectedRevision: current.Task.Revision}, &started); err != nil {
		return "", err
	}
	if task.OwnerRoleID == "pipeline-expert" {
		return "complete", call("task.check", workservice.TaskTransitionRequest{Key: "child-result", TaskID: task.ID,
			ExpectedRevision: started.Revision}, nil)
	}
	if err := call("task.assign", workservice.AssignTaskRequest{Key: "child", Title: "Gather the result", OwnerRoleID: "pipeline-expert",
		ParentTaskID: task.ID, ParentRevision: started.Revision, Condition: work.DependencyResultAvailable}, nil); err != nil {
		return "", err
	}
	return "unfinished", call("task.wait", workservice.TaskTransitionRequest{Key: "wait-for-child", TaskID: task.ID,
		ExpectedRevision: started.Revision}, nil)
}

func (call pipelineCollaborationCLI) unblockParent(input pipelineWorkInput) error {
	if input.RelatedTask == nil || input.Message.Source != (work.Actor{Kind: work.ActorSystem, ID: "dependency-result"}) ||
		input.Message.DerivedFrom == nil {
		return fmt.Errorf("missing independent coordination route or source: %+v", input)
	}
	var parent, child workservice.TaskView
	if err := call("task.inspect", workservice.TaskInspectRequest{TaskID: input.RelatedTask.ID}, &parent); err != nil {
		return err
	}
	source := *input.Message.DerivedFrom
	if err := call("task.inspect", workservice.TaskInspectRequest{TaskID: source.ID}, &child); err != nil {
		return err
	}
	if parent.Task.State != work.TaskWaiting || len(parent.Dependencies) != 1 || parent.Dependencies[0].SatisfiedBy != nil ||
		child.Task.State != work.TaskChecking || source.Version != strconv.FormatInt(child.Task.Revision, 10) {
		return fmt.Errorf("coordination cannot confirm the exact current dependency: parent=%+v child=%+v source=%+v", parent, child, source)
	}
	req := workservice.TaskDependencyRequest{Key: "settle-result", TaskID: parent.Task.ID, ExpectedRevision: parent.Task.Revision,
		DependsOnTaskID: child.Task.ID, DependsOnRevision: child.Task.Revision, Condition: work.DependencyResultAvailable, Source: &source}
	wrong := req
	wrongSource := source
	wrongSource.Version = "0"
	wrong.Key, wrong.Source = "stale-result", &wrongSource
	if err := call("task.dependency.satisfy", wrong, nil); err == nil {
		return fmt.Errorf("stale child result unblocked the parent")
	}
	return call("task.dependency.satisfy", req, nil)
}

func checkPipelineDependency(t *testing.T, store *workstore.Store, scopeID string) {
	t.Helper()
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		tasks, err := tx.ListTasks(scopeID)
		if err != nil {
			return err
		}
		if len(tasks) != 2 {
			return fmt.Errorf("expected parent and delegated task, got %+v", tasks)
		}
		var parent work.Task
		for _, task := range tasks {
			if task.OwnerRoleID == "pipeline-role" {
				parent = task
			}
		}
		dependencies, err := tx.ListDependencies(parent.ID)
		if err != nil {
			return err
		}
		if parent.State != work.TaskDone || len(dependencies) != 1 || dependencies[0].SatisfiedBy == nil {
			return fmt.Errorf("dependency not completed: parent=%+v dependencies=%+v", parent, dependencies)
		}
		inbox, err := tx.ListInbox(scopeID)
		if err != nil {
			return err
		}
		seenCoordination, resumed := false, false
		for _, input := range inbox {
			if input.State != work.InboxProcessed {
				return fmt.Errorf("unprocessed dependency input: %+v", input)
			}
			if input.RelatedTaskID == parent.ID && input.TaskID == "" {
				seenCoordination = true
			}
			if input.TaskID == parent.ID && strings.HasPrefix(input.EventID, "dependency-cleared:") {
				resumed = true
			}
		}
		if !seenCoordination || !resumed {
			return fmt.Errorf("expected both a coordination wake and actual parent continuation: %+v", inbox)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
