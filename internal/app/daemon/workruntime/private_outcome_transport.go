package workruntime

import (
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// These are delivery/read receipts, not unfinished business mutations. Require
// their registered payload and owner rather than trusting an operation kind.
func privateOutcomeTransportOnly(tx *workstore.Tx, operation work.Operation) (bool, error) {
	body, err := tx.GetContent(operation.ScopeID, operation.RequestRef)
	if err != nil {
		return false, err
	}
	if body.SHA256 != operation.RequestHash {
		return false, workstore.ErrScopeDenied
	}
	scope, err := tx.GetScope(operation.ScopeID)
	if err != nil {
		return false, err
	}
	if scope.Kind != work.ScopePersonal {
		return false, nil
	}
	if operation.Kind == "feishu.publish" && operation.Actor == (work.Actor{Kind: work.ActorSystem, ID: "team-ui"}) {
		projection, err := workservice.ReadMessageProjection(tx, operation)
		if err != nil {
			return false, err
		}
		if projection.UIViewID == "" || len(projection.Card) == 0 || projection.Media != nil {
			return false, nil
		}
		view, err := tx.GetUIView(projection.UIViewID)
		if errors.Is(err, workstore.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if view.ScopeID != scope.ID || view.ContextID != operation.ContextID || view.OperationID != operation.ID || view.BodyRef != body.ID || view.PrincipalID != scope.PrincipalID || view.Binding.RoleID != scope.RoleID || view.Binding.RoleID != projection.RoleID || view.Binding.AppID != projection.AppID || view.Binding.GatewayRef != projection.GatewayRef || view.Binding.IdentityGeneration != projection.BotGeneration || view.Binding.IdentityGeneration != operation.TargetGeneration || view.ChatID != operation.TargetID || view.ChatID != projection.ChatID || view.Tenant != projection.Tenant {
			return false, nil
		}
		if view.ReplacesViewID != "" {
			previous, err := tx.GetUIView(view.ReplacesViewID)
			if err != nil {
				return false, err
			}
			return previous.ScopeID == view.ScopeID && previous.PrincipalID == view.PrincipalID && previous.ChatID == view.ChatID && previous.Binding == view.Binding && previous.ExternalMessageID != "" && projection.ReplaceMessageID == previous.ExternalMessageID && projection.ReplyToMessageID == "", nil
		}
		return projection.ReplaceMessageID == "" && projection.ReplyToMessageID == view.ReplyToMessageID, nil
	}
	if operation.Kind == "private.goal.intent-ui" && operation.Actor == (work.Actor{Kind: work.ActorSystem, ID: "session-goal-ui"}) {
		var intentID string
		if err := json.Unmarshal(body.Data, &intentID); err != nil {
			return false, err
		}
		if intentID != operation.TargetID || operation.TargetGeneration != 1 || len(operation.Steps) != 1 || operation.Steps[0].ID != "ui_pending" || operation.Steps[0].State != work.OperationConfirmed {
			return false, nil
		}
		intent, err := tx.GetSessionGoalIntent(intentID)
		if err != nil {
			return false, err
		}
		if intent.ScopeID != scope.ID || intent.ContextID != operation.ContextID {
			return false, nil
		}
		input, err := tx.GetInbox(intent.InputID)
		if err != nil {
			return false, err
		}
		if input.ScopeID != scope.ID || input.RecipientContextID != intent.ContextID || input.SessionGoalIntentID != intent.ID {
			return false, nil
		}
		view, err := tx.GetUIView(operation.Steps[0].Receipt)
		if err != nil {
			return false, err
		}
		if view.ScopeID != scope.ID || view.ContextID != intent.ContextID || view.PrincipalID != scope.PrincipalID || view.Kind != "private.navigation" {
			return false, nil
		}
		uiOperation, err := tx.GetOperation(view.OperationID)
		if err != nil {
			return false, err
		}
		return privateOutcomeTransportOnly(tx, uiOperation)
	}
	expectedKind := agentproto.CommandKind(operation.Kind)
	if operation.Kind == "native.user.mcp.read" {
		if operation.Actor != (work.Actor{Kind: work.ActorSystem, ID: "user-session-mcp"}) {
			return false, nil
		}
		expectedKind = agentproto.CommandRuntimeMCPRead
	}
	if operation.Kind == "native.user.goal" {
		if operation.Actor != (work.Actor{Kind: work.ActorSystem, ID: "user-session-goal"}) && operation.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) {
			return false, nil
		}
		expectedKind = agentproto.CommandThreadGoalGet
	}
	switch expectedKind {
	case agentproto.CommandRuntimeInspect, agentproto.CommandRuntimeMCPRead, agentproto.CommandModelList, agentproto.CommandThreadGoalGet:
	default:
		return false, nil
	}
	var command agentproto.Command
	if err := json.Unmarshal(body.Data, &command); err != nil {
		return false, err
	}
	if command.CommandID != operation.ID || command.Kind != expectedKind || command.Execution == nil || command.Execution.ValidateOwner() != nil || command.Execution.ScopeID != scope.ID || command.Execution.ContextID != operation.ContextID || command.Execution.RoleID != scope.RoleID || command.Execution.WorkerSessionID != operation.TargetID || command.Execution.SessionGeneration != operation.TargetGeneration {
		return false, nil
	}
	if command.Kind == agentproto.CommandThreadGoalGet && command.Target.ThreadID == "" {
		return false, nil
	}

	if command.Kind == agentproto.CommandRuntimeMCPRead && (command.RuntimeMCP == nil || command.RuntimeMCP.Version != agentproto.RuntimeMCPProtocolVersion || command.RuntimeMCP.Mode != "status" && command.RuntimeMCP.Mode != "configuration") {
		return false, nil
	}
	return true, nil
}
