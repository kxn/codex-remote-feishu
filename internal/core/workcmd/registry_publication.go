package workcmd

func publicationCommands() []Command {
	command := func(name, summary, scope string, fields []InputField) Command {
		return Command{Name: name, Version: 1, CredentialPurpose: "worker", Summary: summary, Scope: scope, InputFields: fields, Result: "status, operation_id, object_id, revision, replay"}
	}
	return []Command{
		{Name: "commands", Version: 1, CredentialPurpose: "worker_discovery", Summary: "List commands available to the current managed run.", Scope: "credential-bound current run", Result: "available commands and bounded publication capabilities"},
		command("publication.grant", "Authorize one publication for the expert’s task in this project.", "current project leader", []InputField{{"key", "Stable key."}, {"grantee_role_id", "Current task owner."}, {"task_id", "Exact task in this project."}, {"content_kind", "message or delivery."}, {"references", "For delivery, list the allowed saved resource and version IDs."}}),
		command("publication.revoke", "Withdraw a prior publication delegation.", "current project leader", []InputField{{"key", "Stable key."}, {"grant_id", "Delegation created in this project."}}),
		command("media.publish", "Send a file, image or video to this private conversation.", "bound private work context", []InputField{{"key", "Stable key."}, {"path", "Temporary share: file relative to the current execution directory."}, {"version_id", "Alternatively, an exact saved file version ID."}, {"kind", "file, image, or video."}}),
	}
}

func RequiresPublicationCapability(name string) bool {
	switch name {
	case "message.publish", "delivery.publish", "media.publish", "publication.grant", "publication.revoke":
		return true
	default:
		return false
	}
}
