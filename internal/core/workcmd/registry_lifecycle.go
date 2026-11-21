package workcmd

func lifecycleCommands() []Command {
	mutation := []InputField{{"key", "Stable idempotency key."}, {"task_id", "Task in the current authorized scope."}, {"expected_revision", "Exact current task revision."}}
	result := "status, operation_id, object_id, revision, replay; task completion is independent of native turn completion."
	command := func(name, summary string, fields []InputField) Command {
		return Command{Name: name, Version: 1, CredentialPurpose: "worker", Scope: "credential-bound scope and current explicit run", Summary: summary, InputFields: fields, Result: result}
	}
	commands := append([]Command{}, managementCommands()...)
	commands = append(commands, []Command{
		command("execution.locations", "List only locations explicitly selected by the human for this scope.", nil),
		command("execution.location.use", "Verify an Agent-prepared directory/worktree and select it for the next execution; the active native cwd does not change.", []InputField{{"key", "Stable idempotency key."}, {"root_location_id", "Previously human-selected root location."}, {"path", "Existing Agent-prepared path under that root; the host verifies instead of creating it."}, {"expected_revision", "Expected execution preference revision."}}),
		command("task.list", "Discover tasks in this work scope with bounded pagination.", []InputField{{"limit", "Default 20, maximum 100."}, {"after", "Previous next_cursor."}, {"state", "Optional exact business state."}, {"owner_role_id", "Optional role filter."}}),
		command("task.inspect", "Read one task, adopted requirements, evidence, dependencies, and independent gates.", []InputField{{"task_id", "Authorized task ID."}}),
		command("task.start", "Record business work as active; this does not start a native process.", mutation),
		command("task.wait", "Record business waiting; occupied native carriers are not declared quiescent.", mutation),
		command("task.check", "Submit the task for its explicitly adopted checks.", mutation),
		command("task.done", "Finish only after current required evidence and dependencies are satisfied; no human acceptance is invented.", mutation),
		command("task.cancel", "Cancel business work while preserving history and native occupancy evidence.", mutation),
		command("task.pause", "Close the task's own dispatch gate without clearing other pause sources.", mutation),
		command("task.resume", "Reopen only this task's gate; human and project gates remain authoritative.", mutation),
		command("task.evidence.submit", "Save a version-bound evidence submission; it does not count as adopted evidence.", []InputField{{"key", "Stable submission key."}, {"task_id", "Authorized task."}, {"expected_revision", "Exact task revision."}, {"requirements_revision", "Exact current adopted requirements revision."}, {"assignment_generation", "Exact current assignment generation."}, {"requirement_id", "Adopted requirement ID."}, {"requirement_version", "Exact adopted source version."}, {"kind", "Declared evidence kind."}, {"source", "Exact evidence source/version reference; records a role assertion, not external delivery or human acceptance."}}),
		command("task.evidence.list", "List immutable submissions separately from adopted completion evidence.", []InputField{{"task_id", "Authorized task."}, {"limit", "Default 20, maximum 100."}, {"after", "Previous next_cursor."}}),
	}...)
	adopt := []InputField{{"key", "Stable adoption key."}, {"task_id", "Authorized task."}, {"expected_revision", "Exact task revision."}, {"submission_id", "Saved submission operation ID; old requirement/assignment evidence cannot be adopted."}, {"source_message_id", "Optional actual human conclusion admitted to this run; empty records this role's review."}}
	commands = append(commands, command("task.evidence.adopt", "Adopt a current saved submission as the authenticated responsible actor.", adopt), command("task.review", "Record a responsible review of an exact current submission; real human review needs its actual input.", adopt))
	dependency := []InputField{{"key", "Stable dependency operation key."}, {"task_id", "Authorized parent task."}, {"expected_revision", "Exact parent revision."}, {"depends_on_task_id", "Same-scope dependency task."}, {"depends_on_revision", "Exact dependency task revision."}, {"condition", "result_available, review_accepted, or integrated."}, {"source", "Required satisfaction evidence; omitted when adding an unsatisfied dependency."}}
	return append(commands, command("task.dependency.add", "Add an unsatisfied same-scope dependency; cycles are rejected.", dependency), command("task.dependency.satisfy", "Record an explicit version-bound dependency conclusion.", dependency))
}
