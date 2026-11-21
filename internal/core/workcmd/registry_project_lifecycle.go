package workcmd

func projectLifecycleCommands() []Command {
	command := func(name, summary string, fields []InputField) Command {
		return Command{Name: name, Version: 1, CredentialPurpose: "worker", Summary: summary, Scope: "current project and explicit run; changes require its actual admitted human instruction", InputFields: fields, Result: "status, operation_id, object_id, revision, replay; accepted preserves pending native/platform preparation."}
	}
	mutation := []InputField{{"key", "Stable lifecycle key."}, {"project_id", "Current authorized project."}, {"expected_revision", "Exact project revision."}, {"source_message_id", "Actual human lifecycle instruction admitted to this run."}}
	member := []InputField{{"key", "Stable member-change key."}, {"project_id", "Current project."}, {"expected_revision", "Exact project revision."}, {"role_id", "Selected roster role."}, {"expected_member_revision", "Exact membership revision, or zero creates."}, {"replacement_role_id", "On removal: explicit ready successor for unfinished tasks/decisions; omitted on addition."}, {"source_message_id", "Actual current human instruction."}}
	return []Command{
		command("project.inspect", "Inspect project business state, members, and independent admission gates.", []InputField{{"project_id", "Current project."}}),
		command("project.pause", "Close project admission and record pausing; native quiescence is reconciled separately.", mutation),
		command("project.resume", "Reopen the project gate after fully paused; task/human gates remain authoritative.", mutation),
		command("project.archive", "Record archiving and preserve resources; unknown external work or live occupancy prevents settled archive.", mutation),
		command("project.reopen", "Reopen an archived project into paused state for explicit review and resume.", mutation),
		command("project.member.add", "Record a selected member and queue its own durable Bot membership/native preparation saga.", member),
		command("project.member.remove", "Close new assignments, hand unfinished work to the selected successor, and preserve old native occupancy/history.", member),
		command("project.leader.change", "Transfer coordination, participation agreements, and open business decisions to a ready project role.", []InputField{{"key", "Stable transfer key."}, {"project_id", "Current project."}, {"expected_revision", "Exact project revision."}, {"leader_role_id", "Explicit ready successor role."}, {"source_message_id", "Actual current human leadership instruction."}}),
	}
}
