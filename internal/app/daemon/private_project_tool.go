package daemon

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

const privateProjectToolName = "private_project"

func privateProjectDefinition() toolDefinition {
	return toolDefinition{Name: privateProjectToolName, Description: "Explicit access to the user's currently authorised projects. List projects, list resources and saved versions, read an exact saved version, or prepare a bounded share draft. Preparing a draft never publishes it; the person must open /project and confirm the exact project and content on their own card. Do not send private transcript or change the conversation's workspace or identity.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string", "enum": []string{"list", "resources", "versions", "read", "prepare_share"}}, "project_id": map[string]any{"type": "string"}, "resource_id": map[string]any{"type": "string"}, "version_id": map[string]any{"type": "string"}, "after": map[string]any{"type": "string"}, "operation_id": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}, "memory_id": map[string]any{"type": "string"}, "memory_revision": map[string]any{"type": "integer"}}, "required": []string{"action"}, "additionalProperties": false}}
}
func (a *App) privateProjectTool(ctx context.Context, args map[string]any) (any, *toolError) {
	caller, ok := ctx.Value(privateToolCallerKey{}).(*privateToolCaller)
	if !ok || caller == nil || caller.runtime == nil || caller.runtime.host == nil || a.applications == nil || a.applications.router == nil {
		return nil, &toolError{Code: "private_turn_required", Message: "An authenticated private-chat turn is required."}
	}
	owner, generation := caller.record.Surface, caller.record.SurfaceGeneration
	native := caller.record.Snapshot.Owner
	facade := privateProjectFacade{app: a, router: a.applications.router, journal: caller.runtime.host.journal}
	authority, err := facade.authority(ctx, owner, generation, ingressowner.Owner{Application: ingressowner.Private, ID: native.OwnerID, Epoch: native.Epoch})
	if err != nil {
		return nil, &toolError{Code: "project_identity_unavailable", Message: "Could not verify your current private identity. Send a message with this bot, then try again."}
	}
	action, _ := args["action"].(string)
	project, _ := args["project_id"].(string)
	resource, _ := args["resource_id"].(string)
	version, _ := args["version_id"].(string)
	after, _ := args["after"].(string)
	switch action {
	case "list":
		page, e := facade.projects(ctx, authority, after, 20)
		if e == nil {
			return page, nil
		}
	case "resources":
		page, e := facade.resources(ctx, authority, project, after, 20)
		if e == nil {
			return page, nil
		}
	case "versions":
		items, next, e := facade.versions(ctx, authority, project, resource, after, 20)
		if e == nil {
			return map[string]any{"versions": items, "next_after": next}, nil
		}
	case "read":
		v, e := facade.readVersion(ctx, authority, project, resource, version)
		if e == nil {
			return v, nil
		}
	case "prepare_share":
		operation, _ := args["operation_id"].(string)
		text, _ := args["text"].(string)
		memoryID, _ := args["memory_id"].(string)
		var revision int64
		if memoryID != "" {
			var valid bool
			revision, valid = memoryInteger(args, "memory_revision", false)
			if !valid {
				return nil, &toolError{Code: "invalid_argument", Message: "Select an exact memory revision."}
			}
			memory, e := facade.journal.ReadMemory(ctx, owner, generation, memoryID, revision)
			if e != nil {
				return nil, &toolError{Code: "memory_unavailable", Message: "The exact memory was changed or revoked."}
			}
			text = memory.Content
		}
		if err = facade.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { _, e := facade.authorizedProject(tx, authority, project); return e }); err != nil {
			break
		}
		if resource != "" || version != "" {
			if _, err = facade.readVersion(ctx, authority, project, resource, version); err != nil {
				break
			}
		}
		draft, e := facade.journal.PrepareProjectShare(ctx, owner, generation, operation, privatechat.ProjectShareDraft{ProjectID: project, Text: text, ResourceID: resource, VersionID: version, MemoryID: memoryID, MemoryRevision: revision})
		if e == nil {
			return map[string]any{"draft_id": draft.ID, "status": "awaiting_human_confirmation", "instruction": "Ask the person to open /project and review and confirm this exact draft. Nothing has been shared."}, nil
		}
	default:
		return nil, &toolError{Code: "invalid_argument", Message: "Unknown project action."}
	}
	return nil, &toolError{Code: "project_unavailable", Message: "The project, current membership, selected resource or exact version could not be verified. Refresh the project list and try again."}
}
