package workruntime

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Native final text can be deduplicated against an explicit publication before
// it gets its own display outbox. Keep the native source and same-run exact body
// as proof; a publication by itself never becomes a native processing report.
func privateOutcomeExplicitFinal(tx *workstore.Tx, attempt work.ExecutionAttempt, request InspectionRequest, operations []work.Operation) (work.Content, error) {
	turn, err := tx.GetNativeTurn(request.Carrier.Incarnation, request.SessionID, request.TurnID)
	if err != nil {
		return work.Content{}, err
	}
	if turn.Binding != request.Binding || turn.TerminalStatus != "completed" {
		return work.Content{}, nil
	}
	events, err := tx.NativeFinalMessageEvents(request.Carrier.Incarnation, request.SessionID, request.TurnID, turn.TerminalSequence)
	if err != nil {
		return work.Content{}, err
	}
	var final, last string
	seen := map[string]bool{}
	for _, event := range events {
		if event.TrafficClass == agentproto.TrafficClassInternalHelper || event.Initiator.Kind == agentproto.InitiatorInternalHelper || event.ItemID == "" || seen[event.ItemID] {
			continue
		}
		seen[event.ItemID] = true
		if event.Execution != nil && *event.Execution != request.Binding {
			continue
		}
		if event.CommandID != "" {
			command, err := tx.ResolveNativeCommand(request.Carrier.Incarnation, event.CommandID)
			if err != nil {
				return work.Content{}, err
			}
			if command.Binding != request.Binding {
				continue
			}
		}
		text, _ := event.Metadata["text"].(string)
		text = strings.TrimSpace(text)
		phase, _ := event.Metadata["phase"].(string)
		delivery, _ := event.Metadata["delivery"].(string)
		if text == "" || phase == "commentary" || delivery == "async" {
			continue
		}
		if len(final)+len(text) > 512*1024 {
			return work.Content{}, workstore.ErrScopeDenied
		}
		if phase == "final_answer" || phase == "finalAnswer" || final != "" {
			if final != "" {
				final += "\n\n"
			}
			final += text
		} else {
			last = text
		}
	}
	if final == "" {
		final = last
	}
	if final == "" {
		return work.Content{}, nil
	}
	scope, err := tx.GetScope(attempt.ScopeID)
	if err != nil {
		return work.Content{}, err
	}
	bot, err := tx.GetRoleBotBinding(scope.RoleID)
	if err != nil {
		return work.Content{}, err
	}
	for _, operation := range operations {
		if operation.Actor != (work.Actor{Kind: work.ActorRole, ID: scope.RoleID}) {
			continue
		}
		projection, err := workservice.ReadMessageProjection(tx, operation)
		if err != nil {
			return work.Content{}, err
		}
		if projection.PrivateDisplay != nil || projection.Body == "" || projection.UIViewID != "" || projection.SourceRunRef != attempt.RunRef.RunID || projection.AppID != bot.AppID || projection.GatewayRef != bot.GatewayRef || projection.RoleID != bot.RoleID || projection.BotGeneration != bot.IdentityGeneration || workservice.PrivateResultHash(projection.Body) != workservice.PrivateResultHash(final) {
			continue
		}
		content := work.NewContent("private-native-final:"+attempt.ID, attempt.ScopeID, "text/markdown", []byte(final), operation.CreatedAt)
		return content, tx.PutContent(content)
	}
	return work.Content{}, nil
}
