// Package workcmd describes the shared product commands. Authentication and
// business mutations belong to the application Dispatcher, not this registry.
package workcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const MaxRequestBytes = 1 << 20
const ExecutePath = "/v1/team/execute"

type DispatchRequest struct {
	Name    string          `json:"name"`
	RunRef  string          `json:"run_ref"`
	Payload json.RawMessage `json:"payload"`
}

type Dispatcher interface {
	Execute(context.Context, string, DispatchRequest) (any, error)
}
type DispatcherFunc func(context.Context, string, DispatchRequest) (any, error)

func (f DispatcherFunc) Execute(ctx context.Context, token string, req DispatchRequest) (any, error) {
	return f(ctx, token, req)
}

type InputField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Command struct {
	Name              string       `json:"name"`
	Version           int          `json:"version"`
	Summary           string       `json:"summary"`
	Scope             string       `json:"scope"`
	CredentialPurpose string       `json:"credential_purpose"`
	InputFields       []InputField `json:"input_fields"`
	Result            string       `json:"result"`
}

// Fields are discovery descriptions, not a second validation schema. The
// dispatcher decodes its actual typed DTO with DecodePayload before execution.
var commands = []Command{
	{Name: "control.consume", Version: 1, CredentialPurpose: "worker", Summary: "Acknowledge the original copy of a management-only human input handled by this control run.", Scope: "restricted project management context", InputFields: []InputField{{"key", "Stable idempotency key."}, {"source_message_id", "Exact human message admitted to this control run."}, {"management_operation_id", "Actual confirmed management mutation or recoverable reply operation for this management-only input in the same control context."}}, Result: "Only the linked pending business input is cancelled; other project work remains queued."},
	{Name: "message.publish", Version: 1, CredentialPurpose: "worker", Summary: "Queue an explicit message for this work scope’s bound conversation; accepted is not sent.", Scope: "bound project/personal or restricted bootstrap conversation", InputFields: []InputField{{"key", "Stable publication idempotency key."}, {"body", "Explicit message text."}, {"reply_to_input_id", "Optional actual platform input to reply to."}, {"task_id", "Optional project task the caller owns or leads. With no reply target, the first publication establishes the task topic."}}, Result: "accepted with publication/outbox receipt; no sent/read claim before a platform receipt."},
	{Name: "bootstrap.context", Version: 1, CredentialPurpose: "worker", Summary: "Read only this unbound-chat establishment scope and real team/role candidates.", Scope: "credential-bound bootstrap scope", InputFields: []InputField{}, Result: "Authorized establishment request and candidate configuration; no private or project knowledge."},
	{Name: "bootstrap.location.pick", Version: 1, CredentialPurpose: "worker", Summary: "Ask the initiating human to choose a real execution directory or isolated scratch in the current group.", Scope: "credential-bound bootstrap scope", InputFields: []InputField{{"key", "Stable picker request key."}, {"source_message_id", "Original project request message admitted to the run."}}, Result: "accepted with a persistent chooser that will requeue the original input after the human chooses."},
	{Name: "bootstrap.team.resolve", Version: 1, CredentialPurpose: "worker", Summary: "Find the reusable team of an exact project name referenced by the initiating human.", Scope: "credential-bound bootstrap scope", InputFields: []InputField{{"project_name", "Exact existing project name from the human request."}, {"source_message_id", "Human input that contains that name, admitted to this run."}}, Result: "Verified project-name and team matches only; no project materials or history."},
	{Name: "content.read", Version: 1, CredentialPurpose: "worker", Summary: "Read a bounded page of authorized stored text or JSON.", Scope: "credential-bound work scope", InputFields: []InputField{{"id", "Authorized content ID."}, {"offset", "Character offset, starting at zero."}, {"limit", "Character count, default and maximum 8000."}}, Result: "id, sha256, media_type, text, offset, next_offset, total; does not widen content audience."},
	{Name: "project.create", Version: 1, CredentialPurpose: "bootstrap_or_worker", Summary: "Establish the project selected by this one-chat bootstrap authorization.", Scope: "bootstrap-bound tenant, chat, principal, and establishment operation", InputFields: []InputField{
		{"key", "Must equal the bootstrap credential establishment operation ID."}, {"tenant", "Bootstrap-authorized tenant."}, {"chat_id", "Bootstrap-authorized chat."}, {"principal_id", "Bootstrap-authorized initiating person."}, {"expected_binding_generation", "Expected existing chat binding generation."}, {"name", "Project name."}, {"team_id", "Selected existing team."}, {"leader_role_id", "Bootstrap-authorized leader role."}, {"member_role_ids", "Selected authorized role members."}, {"location_ids", "Locations explicitly selected by the human; empty selects isolated scratch when no selection exists."}, {"default_location_id", "Explicit default among the human-selected locations; never guessed from previous chats."}, {"document_container", "Optional previously selected document container."}, {"source_message_id", "Actual human input already admitted to this bootstrap run; UI form submissions are recorded by the trusted ingress."}, {"initial_request", "Original project request; persisted with the establishment intent."},
	}, Result: "status, operation_id, object_id, revision, replay; accepted means provisioning reserved, not remote setup completed."},
	{Name: "message.send", Version: 1, CredentialPurpose: "worker", Summary: "Send a durable message in this scope; repeated identical exchanges without new human input or task revision return needs_coordination and one leader notice.", Scope: "credential-bound work scope", InputFields: []InputField{
		{"key", "Stable idempotency key."}, {"recipient_role_id", "Enrolled receiving role; unavailable members queue until ready."}, {"recipient_context_id", "Optional exact receiving business context."}, {"conversation_id", "Conversation associated with this message."}, {"task_id", "Optional executable task assigned to the recipient."}, {"related_task_id", "Optional read-only association with a same-project task being discussed; defaults to the sender’s task when different from the recipient’s task."}, {"body", "Message text persisted in the authorized scope."},
	}, Result: "status, operation_id, object_id, revision, replay; persisted receipt is not proof of recipient processing."},
	{Name: "outcome.report", Version: 1, CredentialPurpose: "worker", Summary: "Record exactly which admitted inputs this run processed and its checkpoint.", Scope: "credential-bound run and work scope", InputFields: []InputField{
		{"key", "Stable report idempotency key."}, {"expected_revision", "Expected processing-report revision."}, {"processed_input_ids", "Exact input IDs admitted to this run and actually processed."}, {"summary", "Durable processing summary."}, {"checkpoint", "Work-context continuation checkpoint."}, {"work_disposition", "complete, unfinished, or unknown for the already authorized work; not human acceptance or task completion."},
	}, Result: "status, operation_id, object_id, revision, replay; processing report alone is not runtime quiescence or task completion."},
	{Name: "context", Version: 1, CredentialPurpose: "worker", Summary: "Read this role's current task, admitted inputs, and pending-input count.", Scope: "credential-bound work scope", InputFields: []InputField{}, Result: "Authorized scope, role, context, worker, attempt, current task, admitted inputs, and pending count."},
	{Name: "task.assign", Version: 1, CredentialPurpose: "worker", Summary: "Assign a task to an authorized role; optionally register a parent dependency.", Scope: "credential-bound work scope", InputFields: []InputField{
		{"key", "Stable idempotency key for this assignment."}, {"title", "Task title."}, {"owner_role_id", "Authorized receiving role."}, {"parent_task_id", "Optional parent task."}, {"parent_revision", "Expected parent revision when delegating."}, {"condition", "Explicit dependency condition when delegating."}, {"requirements", "Adopted requirement references and required evidence."}, {"business_source", "Optional external business authority/version reference."},
	}, Result: "status, operation_id, object_id, revision, replay; accepted or pending_external is not completion."},
	{Name: "task.revise", Version: 1, CredentialPurpose: "worker", Summary: "Revise task requirements or responsibility against an expected revision.", Scope: "credential-bound work scope", InputFields: []InputField{
		{"key", "Stable idempotency key for this revision."}, {"task_id", "Existing authorized task."}, {"expected_revision", "Required current task revision."}, {"title", "Resulting task title."}, {"owner_role_id", "Resulting authorized owner role."}, {"requirements", "Resulting adopted requirement references."}, {"business_source", "Optional external business authority/version reference."},
	}, Result: "status, operation_id, object_id, revision, replay; a revision conflict does not apply the change."},
}

func List() []Command {
	all := append(append([]Command{}, commands...), extendedCommands()...)
	all = append(all, resourceWriteCommands()...)
	all = append(all, lifecycleCommands()...)
	all = append(all, projectLifecycleCommands()...)
	result := make([]Command, len(all))
	copy(result, all)
	for i := range result {
		result[i].InputFields = append([]InputField{}, all[i].InputFields...)
	}
	return result
}
func Lookup(name string) (Command, bool) {
	for _, command := range List() {
		if command.Name == name {
			return command, true
		}
	}
	return Command{}, false
}

func (r DispatchRequest) Validate() error {
	command, ok := Lookup(r.Name)
	if !ok {
		return fmt.Errorf("unknown team command %q", r.Name)
	}
	if command.CredentialPurpose == "worker" && strings.TrimSpace(r.RunRef) == "" {
		return fmt.Errorf("explicit run_ref is required")
	}
	if command.CredentialPurpose == "bootstrap" && r.RunRef != "" {
		return fmt.Errorf("bootstrap project creation must not supply run_ref")
	}
	return ValidatePayload(r.Payload)
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
