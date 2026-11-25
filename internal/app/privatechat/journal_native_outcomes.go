package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func nativeEventOutcome(command agentproto.Command, event agentproto.Event) (outcome, method string, definitive bool) {
	if event.CommandID != command.CommandID {
		return "", "", false
	}
	if command.Kind == agentproto.CommandThreadCompactStart && event.Kind == agentproto.EventNativeCommandRejected && event.Status == "failed" && event.Action == "wrapper.before_compact_write" && event.Problem != nil && event.Problem.CommandID == command.CommandID {
		return "failed", "compact-prewrite", true
	}
	if event.Kind == agentproto.EventNativeCommandRejected && event.Status == "failed" && event.Action == "wrapper.before_child_write" && event.Problem != nil && event.Problem.CommandID == command.CommandID {
		return "failed", "prewrite", true
	}
	if command.NativeBinding.Owner.Backend == agentproto.BackendOpenCode && event.Kind == agentproto.EventNativeCommandRejected && event.Status == "failed" && event.Action == "native.rpc.rejected" && event.RequestID != "" && event.Problem != nil && event.Problem.CommandID == command.CommandID {
		switch event.Problem.Operation {
		case "session/new", "session/resume", "session/prompt", "session/list", "session/load", "session/new.catalog":
			method := event.Problem.Operation
			if method == "session/new.catalog" {
				method = "session/new"
			}
			return "failed", method, true
		}
	}
	if command.Kind == agentproto.CommandRuntimeInspect && command.RuntimeInspection != nil && command.NativeBinding.ExecutionID == "" && event.Kind == agentproto.EventRuntimeInspectionCompleted && event.RuntimeInspection != nil && event.RuntimeInspection.RequestCommandID == command.CommandID && event.RuntimeInspection.CarrierIncarnation == command.NativeBinding.Owner.CarrierIncarnation && event.RuntimeInspection.InstanceGeneration == command.NativeBinding.Owner.InstanceGeneration && event.RuntimeInspection.ProcessGeneration == command.NativeBinding.Owner.ProcessGeneration && event.RuntimeInspection.LocationGeneration == 0 {
		return "accepted", "local", true
	}
	if command.NativeBinding.Owner.Backend == agentproto.BackendClaude && command.Kind == agentproto.CommandThreadsRefresh && event.Kind == agentproto.EventThreadsSnapshot {
		return "accepted", "local", true
	}
	if command.NativeBinding.Owner.Backend == agentproto.BackendClaude && command.Kind == agentproto.CommandThreadHistoryRead && event.Kind == agentproto.EventThreadHistoryRead && event.ThreadHistory != nil && event.ThreadID != "" && event.ThreadID == command.Target.ThreadID {
		return "accepted", "local", true
	}
	if outcome, method, ok := nativeQueryOutcome(command, event); ok {
		return outcome, method, true
	}
	if event.RequestID == "" {
		return "", "", false
	}
	switch command.Kind {
	case agentproto.CommandMCPOAuthLogin:
		if command.NativeBinding.Owner.Backend != agentproto.BackendCodex || command.MCP.OAuthLogin == nil || event.ThreadID != command.MCP.OAuthLogin.ThreadID {
			return "", "", false
		}
		if event.Kind == agentproto.EventMCPOAuthLoginURLReady && event.MCPOAuthLogin != nil && event.MCPOAuthLogin.ServerName == command.MCP.OAuthLogin.ServerName && event.MCPOAuthLogin.ThreadID == command.MCP.OAuthLogin.ThreadID && event.MCPOAuthLogin.AuthorizationURL != "" {
			return "accepted", "mcpServer/oauth/login", true
		}
		if event.Kind == agentproto.EventSystemError && event.Problem != nil && event.Problem.CommandID == command.CommandID && event.Problem.Operation == string(agentproto.CommandMCPOAuthLogin) && event.Problem.Stage == "command_response" {
			return "failed", "mcpServer/oauth/login", true
		}
	case agentproto.CommandThreadsRefresh:
		if command.NativeBinding.Owner.Backend == agentproto.BackendOpenCode && event.Kind == agentproto.EventThreadsSnapshot {
			return "accepted", "session/list", true
		}
		if command.NativeBinding.Owner.Backend == agentproto.BackendCodex && event.Kind == agentproto.EventNativeQueryCompleted && event.Action == "thread.list" {
			if event.Status == "completed" {
				return "accepted", "shared", true
			}
			if event.Status == "failed" {
				return "failed", "shared", true
			}
		}
	case agentproto.CommandThreadHistoryRead:
		if command.NativeBinding.Owner.Backend == agentproto.BackendOpenCode && event.Kind == agentproto.EventThreadHistoryRead && event.ThreadHistory != nil && event.ThreadID == command.Target.ThreadID && event.ThreadID != "" {
			return "accepted", "session/load", true
		}
		if command.NativeBinding.Owner.Backend == agentproto.BackendCodex && event.Kind == agentproto.EventThreadHistoryRead && event.ThreadHistory != nil && event.ThreadID != "" && event.ThreadID == command.Target.ThreadID {
			return "accepted", "thread/read", true
		}
	case agentproto.CommandModelList:
		if command.NativeBinding.Owner.Backend == agentproto.BackendOpenCode && event.Kind == agentproto.EventModelCatalogUpdated && event.ModelCatalog != nil {
			return "accepted", "session/new", true
		}
		if command.NativeBinding.Owner.Backend == agentproto.BackendCodex && event.Kind == agentproto.EventModelCatalogUpdated && event.ModelCatalog != nil {
			return "accepted", "model/list", true
		}
	case agentproto.CommandPromptSend:
		if command.NativeBinding.Owner.Backend == agentproto.BackendCodex && event.Kind == agentproto.EventTurnCompleted && event.Status == "failed" && event.TurnID == "" {
			switch event.TurnCompletionOrigin {
			case agentproto.TurnCompletionOriginThreadStartMissingModel:
				if command.Target.EffectivePromptExecutionMode() == agentproto.PromptExecutionModeStartNew && command.Overrides.Model == "" && command.Overrides.PlanMode != "" {
					return "failed", "thread/start", true
				}
			case agentproto.TurnCompletionOriginThreadForkMissingModel:
				if command.Target.EffectivePromptExecutionMode() == agentproto.PromptExecutionModeForkEphemeral && command.Overrides.Model == "" && command.Overrides.PlanMode != "" {
					return "failed", "thread/fork", true
				}
			case agentproto.TurnCompletionOriginThreadResumeMissingModel:
				if command.Target.EffectivePromptExecutionMode() == agentproto.PromptExecutionModeResumeExisting && event.ThreadID == command.Target.ThreadID && command.Overrides.Model == "" && command.Overrides.PlanMode != "" {
					return "failed", "thread/resume", true
				}
			case agentproto.TurnCompletionOriginThreadStartRejected:
				if command.Target.ExecutionMode == agentproto.PromptExecutionModeStartNew {
					return "failed", "thread/start", true
				}
			case agentproto.TurnCompletionOriginThreadResumeRejected:
				if command.Target.ExecutionMode == agentproto.PromptExecutionModeResumeExisting && event.ThreadID == command.Target.ThreadID {
					return "failed", "thread/resume", true
				}
			case agentproto.TurnCompletionOriginTurnStartRejected:
				if command.Target.ThreadID == "" || event.ThreadID == command.Target.ThreadID {
					return "failed", "turn/start", true
				}
			}
		}
		if event.ThreadID == "" || event.TurnID == "" || command.Target.ThreadID != "" && event.ThreadID != command.Target.ThreadID {
			return "", "", false
		}
		if command.NativeBinding.Owner.Backend == agentproto.BackendCodex && event.Kind == agentproto.EventNativeTurnBindingObserved && event.Status == "accepted" {
			return "accepted", "turn/start", true
		}
		if command.NativeBinding.Owner.Backend == agentproto.BackendOpenCode && event.Kind == agentproto.EventNativeTurnBindingObserved && event.Status == "accepted" {
			return "accepted", "session/prompt", true
		}
		if command.NativeBinding.Owner.Backend == agentproto.BackendClaude && event.Kind == agentproto.EventTurnStarted && event.Metadata["turnIdSource"] == "synthetic_from_observed_user_uuid" && event.Metadata["nativeUserUUID"] == event.RequestID && event.TurnID == "relay-claude-turn-"+event.RequestID {
			return "accepted", "user", true
		}
	case agentproto.CommandRequestRespond:
		if event.Kind == agentproto.EventNativeControlResult && event.Action == "request.response.written" && event.Status == "sent" && command.Request.RequestID != "" && event.RequestID == command.Request.RequestID && event.ThreadID == command.Target.ThreadID && event.TurnID == command.Target.TurnID {
			return "sent", "request-written", true
		}
	case agentproto.CommandReviewStart:
		if command.NativeBinding.Owner.Backend != agentproto.BackendCodex || event.Kind != agentproto.EventNativeControlResult || event.Action != "review.start" || event.ThreadID == "" || event.ThreadID != command.Target.ThreadID {
			return "", "", false
		}
		if event.Status == "accepted" {
			return "accepted", "review/start", true
		}
		if event.Status == "failed" {
			if stage, ok := event.Metadata["stage"].(string); ok {
				switch stage {
				case "thread/start", "review/start":
					return "failed", stage, true
				default:
					return "", "", false
				}
			}
			return "failed", "review/start", true
		}
	case agentproto.CommandThreadCompactStart:
		if command.NativeBinding.Owner.Backend == agentproto.BackendCodex && event.Kind == agentproto.EventSystemError && event.Problem != nil && event.Problem.CommandID == command.CommandID && event.Problem.Operation == "thread.compact.start" && event.Problem.Stage == "thread_resume_response" && event.ThreadID == command.Target.ThreadID {
			return "failed", "thread/resume", true
		}
		if command.NativeBinding.Owner.Backend != agentproto.BackendCodex || event.Kind != agentproto.EventNativeControlResult || event.Action != "thread.compact.start" || event.ThreadID == "" || event.ThreadID != command.Target.ThreadID {
			return "", "", false
		}
		if event.Status == "accepted" {
			return "accepted", "thread/compact/start", true
		}
		if event.Status == "failed" {
			return "failed", "thread/compact/start", true
		}
	case agentproto.CommandThreadShellCommand:
		if command.NativeBinding.Owner.Backend != agentproto.BackendCodex || event.Kind != agentproto.EventNativeShellRPCResult || event.ThreadID == "" || event.ThreadID != command.Target.ThreadID || event.Metadata["shellOperationID"] != command.ShellCommand.OperationID {
			return "", "", false
		}
		if event.Status == "acknowledged" {
			return "accepted", "thread/shellCommand", true
		}
		if event.Status == "failed" {
			return "failed", "thread/shellCommand", true
		}
	case agentproto.CommandTurnInterrupt:
		if event.Kind != agentproto.EventNativeControlResult || event.Action != "turn.interrupt" || event.ThreadID != command.Target.ThreadID || event.TurnID != command.Target.TurnID {
			return "", "", false
		}
		method := "turn/interrupt"
		if command.NativeBinding.Owner.Backend == agentproto.BackendClaude {
			method = "interrupt"
		}
		if event.Status == "accepted" {
			return "accepted", method, true
		}
		if event.Status == "failed" {
			return "failed", method, true
		}
	case agentproto.CommandTurnSteer:
		if event.ThreadID != command.Target.ThreadID || event.TurnID != command.Target.TurnID {
			return "", "", false
		}
		method = "turn/steer"
		if command.NativeBinding.Owner.Backend == agentproto.BackendClaude {
			method = "user"
		}
		if event.Kind == agentproto.EventNativeTurnInputAccepted && event.Status == "accepted" {
			return "accepted", method, true
		}
		if event.Kind == agentproto.EventNativeTurnInputRejected && event.Status == "failed" {
			return "failed", method, true
		}
	}
	return "", "", false
}

// Resolution establishes only that the native request closed. It does not
// claim which actor's answer the backend consumed.
func resolvePrivateRequestResponseTx(ctx context.Context, tx *sql.Tx, l *NativeLease, event agentproto.Event) error {
	if event.RequestID == "" {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT d.id,n.payload,d.event_sequence FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id WHERE d.carrier=? AND d.state='sent' AND json_extract(n.payload,'$.kind')=? AND json_extract(n.payload,'$.request.requestId')=?`, l.native.CarrierIncarnation, string(agentproto.CommandRequestRespond), event.RequestID)
	if err != nil {
		return err
	}
	type candidate struct {
		id           string
		command      agentproto.Command
		sentSequence int64
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		var raw []byte
		if err = rows.Scan(&c.id, &raw, &c.sentSequence); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal(raw, &c.command) != nil {
			rows.Close()
			return ErrJournalConflict
		}
		if c.command.NativeBinding == nil || c.command.NativeBinding.Owner != l.native {
			rows.Close()
			return ErrJournalConflict
		}
		if event.ThreadID != "" && c.command.Target.ThreadID != event.ThreadID || event.TurnID != "" && c.command.Target.TurnID != event.TurnID {
			continue
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	var matched string
	for _, c := range candidates {
		var started sql.NullInt64
		err = tx.QueryRowContext(ctx, `SELECT max(sequence) FROM native_events WHERE incarnation=? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.requestId')=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? AND sequence<?`, l.native.CarrierIncarnation, string(agentproto.EventRequestStarted), event.RequestID, c.command.Target.ThreadID, c.command.Target.TurnID, c.sentSequence).Scan(&started)
		if err != nil {
			return err
		}
		if !started.Valid || int64(event.Seq) <= started.Int64 {
			continue
		}
		if matched != "" {
			return ErrJournalConflict
		}
		matched = c.id
	}
	if matched != "" {
		_, err = tx.ExecContext(ctx, `UPDATE dispatch SET state='resolved' WHERE id=? AND state='sent'`, matched)
	}
	return err
}

// A later sequence may arrive over the wire before its prior sent fact.
func resolveAlreadyReceivedPrivateRequestTx(ctx context.Context, tx *sql.Tx, l *NativeLease, sent agentproto.Event) error {
	var started sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(sequence) FROM native_events WHERE incarnation=? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.requestId')=? AND sequence<=?`, l.native.CarrierIncarnation, string(agentproto.EventRequestStarted), sent.RequestID, sent.Seq).Scan(&started); err != nil {
		return err
	}
	if !started.Valid {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM native_events WHERE incarnation=? AND sequence>? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.requestId')=? ORDER BY sequence`, l.native.CarrierIncarnation, started.Int64, string(agentproto.EventRequestResolved), sent.RequestID)
	if err != nil {
		return err
	}
	var events []agentproto.Event
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var event agentproto.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			rows.Close()
			return err
		}
		events = append(events, event)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, event := range events {
		if err = resolvePrivateRequestResponseTx(ctx, tx, l, event); err != nil {
			return err
		}
	}
	return nil
}

func resolvePendingWrittenForRequestTx(ctx context.Context, tx *sql.Tx, l *NativeLease, started agentproto.Event) error {
	if started.RequestID == "" {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT e.payload FROM native_events e JOIN native_commands n ON n.command_id=e.command_id JOIN dispatch d ON d.id=n.dispatch_id WHERE e.incarnation=? AND d.state='sent' AND json_extract(e.payload,'$.action')='request.response.written' AND json_extract(e.payload,'$.requestId')=?`, l.native.CarrierIncarnation, started.RequestID)
	if err != nil {
		return err
	}
	var sent []agentproto.Event
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var event agentproto.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			rows.Close()
			return err
		}
		sent = append(sent, event)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, event := range sent {
		if err = resolveAlreadyReceivedPrivateRequestTx(ctx, tx, l, event); err != nil {
			return err
		}
	}
	return nil
}
