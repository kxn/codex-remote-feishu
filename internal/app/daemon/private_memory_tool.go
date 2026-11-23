package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

const privateMemoryToolName = "private_memory"

func privateMemoryDefinition() toolDefinition {
	return toolDefinition{Name: privateMemoryToolName, Description: "Private conversation memory, retained across session/model/workspace changes. When prior decisions or preferences may help, list the index and read only relevant exact revisions. Save durable user preferences and agreed decisions with their applicability; do not save secrets, transient progress, or unrelated project/team information. User corrections replace or revoke an entry. Write/revoke require an exact expected_revision (0 only for a new ID) and a stable operation_id reused when retrying the same mutation. This tool does not publish anything to chat.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"list", "read", "write", "revoke"}},
		"after":  map[string]any{"type": "string"}, "id": map[string]any{"type": "string"}, "revision": map[string]any{"type": "integer"},
		"operation_id": map[string]any{"type": "string"}, "expected_revision": map[string]any{"type": "integer"},
		"applicability": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"},
	}, "required": []string{"action"}, "additionalProperties": false}}
}

func memoryInteger(args map[string]any, key string, allowZero bool) (int64, bool) {
	n, ok := args[key].(float64)
	if !ok || n < 0 || n > 1<<53-1 || (!allowZero && n == 0) {
		return 0, false
	}
	v := int64(n)
	return v, n == float64(v)
}

func (a *App) privateMemoryTool(ctx context.Context, args map[string]any) (any, *toolError) {
	caller, ok := ctx.Value(privateToolCallerKey{}).(*privateToolCaller)
	if !ok || caller == nil || caller.runtime == nil || caller.runtime.host == nil || caller.runtime.host.journal == nil {
		return nil, &toolError{Code: "private_turn_required", Message: "Authenticated private-chat context is required."}
	}
	j := caller.runtime.host.journal
	owner, generation := caller.record.Surface, caller.record.SurfaceGeneration
	action, _ := args["action"].(string)
	id, _ := args["id"].(string)
	switch action {
	case "list":
		after, _ := args["after"].(string)
		items, next, err := j.ListMemoryIndex(ctx, owner, generation, after, 20)
		if err != nil {
			return nil, &toolError{Code: "memory_unavailable", Message: "Private memory index could not be read."}
		}
		return map[string]any{"memories": items, "next_after": next}, nil
	case "read":
		revision, ok := memoryInteger(args, "revision", false)
		if id == "" || !ok {
			return nil, &toolError{Code: "invalid_argument", Message: "A memory ID and its exact revision are required."}
		}
		entry, err := j.ReadMemory(ctx, owner, generation, id, revision)
		if err != nil {
			return nil, &toolError{Code: "memory_not_found", Message: "That memory was changed, revoked or is unavailable. Refresh the index before reading."}
		}
		if len(entry.Content) > 64<<10 || !utf8.ValidString(entry.Content) {
			return nil, &toolError{Code: "memory_unavailable", Message: "This memory cannot be displayed as text."}
		}
		return entry, nil
	case "write", "revoke":
		expected, ok := memoryInteger(args, "expected_revision", true)
		if !ok {
			return nil, &toolError{Code: "invalid_argument", Message: "expected_revision must be an exact nonnegative integer; use 0 only for a new entry."}
		}
		operation, _ := args["operation_id"].(string)
		content, _ := args["content"].(string)
		applicability, _ := args["applicability"].(string)
		sources, _ := json.Marshal([]any{map[string]any{"kind": "private_agent", "carrier": caller.record.Snapshot.Owner.CarrierIncarnation, "backend": caller.record.Snapshot.Owner.Backend, "instance": caller.record.Snapshot.Owner.InstanceID}})
		entry, err := j.MutateMemory(ctx, owner, generation, privatechat.MemoryMutation{OperationID: operation, ID: id, ExpectedRevision: expected, Revoke: action == "revoke", Content: content, Applicability: applicability}, sources)
		if err != nil {
			return nil, &toolError{Code: "memory_write_rejected", Message: "Memory was not changed. Supply a stable operation_id, the current revision, and nonempty applicability/content for write. Refresh the index if another update occurred."}
		}
		// A replay acknowledges its original write without exposing a revoked body.
		return map[string]any{"id": entry.ID, "revision": entry.Revision, "state": entry.State}, nil
	default:
		return nil, &toolError{Code: "invalid_argument", Message: fmt.Sprintf("Unknown memory action %q.", action)}
	}
}
