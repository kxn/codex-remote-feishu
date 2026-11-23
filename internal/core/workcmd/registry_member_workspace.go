package workcmd

func memberWorkspaceCommands() []Command {
	return []Command{{Name: "member.workspace.inspect", Version: 1, CredentialPurpose: "worker", Summary: "Inspect this project member's durable workspace and authorized recovery references.", Scope: "current project and active member", InputFields: []InputField{{Name: "limit", Description: "Default 20, maximum 100."}, {Name: "after", Description: "Previous next_cursor."}}, Result: "Workspace path and paginated task, context, memory and resource references; details remain subject to their original authorization."}}
}
