// Package workdisplay projects durable private native turns into the team
// outbox. Native text is presentation evidence only.
package workdisplay

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func IsOutput(event agentproto.Event) bool {
	switch event.Kind {
	case agentproto.EventItemDelta, agentproto.EventItemReasoningSummaryPartAdded:
		public, _ := event.Metadata["publicReasoningSummary"].(bool)
		return public && event.ItemKind == "reasoning_summary" && event.Delta != ""
	case agentproto.EventThreadTokenUsageUpdated:
		return event.TokenUsage != nil
	case agentproto.EventTurnPlanUpdated, agentproto.EventTurnDiffUpdated, agentproto.EventItemFileChangePatchUpdated, agentproto.EventSystemError:
		return true
	case agentproto.EventItemStarted, agentproto.EventItemCompleted:
		switch event.ItemKind {
		case "agent_message", "plan", "reasoning_summary", "reasoning", "command_execution", "file_change", "context_compaction", "web_search", "mcp_tool_call", "dynamic_tool_call", "delegated_task", "image_generation":
			return true
		}
	}
	return false
}

func Apply(tx *workstore.Tx, carrier workstore.CarrierRegistration, binding agentproto.ExecutionBinding, event agentproto.Event, now time.Time) error {
	scope, err := tx.GetScope(binding.ScopeID)
	if err != nil || scope.Kind != work.ScopePersonal {
		return err
	}
	context, err := tx.GetContext(binding.ContextID)
	if err != nil || context.Purpose == work.PurposeReview || context.Purpose == work.PurposeControl && (event.Kind != agentproto.EventTurnCompleted || event.Status != "failed") {
		return err
	}
	if event.TurnID == "" {
		switch event.TurnCompletionOrigin {
		case agentproto.TurnCompletionOriginThreadStartRejected, agentproto.TurnCompletionOriginThreadResumeRejected, agentproto.TurnCompletionOriginTurnStartRejected:
			event.ThreadID = xutil.FirstNonEmpty(event.ThreadID, "unstarted-session")
			event.TurnID = "rejected:" + event.CommandID
		default:
			return nil
		}
	}
	if event.ThreadID == "" {
		return nil
	}
	id := stableID("display", carrier.Incarnation, event.ThreadID, event.TurnID, binding.AttemptID)
	display, err := tx.GetPrivateExecutionDisplay(id)
	if errors.Is(err, workstore.ErrNotFound) {
		display, err = begin(tx, id, scope, carrier, binding, event)
	}
	if err != nil || display.ID == "" || display.Terminated {
		return err
	}
	if display.Binding != binding {
		return workstore.ErrScopeDenied
	}
	display.Progress.Verbosity, err = executionVerbosity(tx, binding.ContextID)
	if err != nil {
		return err
	}
	p := projection{tx: tx, display: &display, event: event, backend: carrier.Backend, now: now}
	if err := p.apply(); err != nil {
		return err
	}
	previous := display.Revision
	display.Revision++
	return tx.PutPrivateExecutionDisplay(display, previous)
}

func begin(tx *workstore.Tx, id string, scope work.WorkScope, carrier workstore.CarrierRegistration, binding agentproto.ExecutionBinding, event agentproto.Event) (workstore.PrivateExecutionDisplay, error) {
	var display workstore.PrivateExecutionDisplay
	attempt, err := tx.GetAttempt(binding.AttemptID)
	if err != nil {
		return display, err
	}
	bot, err := tx.GetRoleBotBinding(binding.RoleID)
	if errors.Is(err, workstore.ErrNotFound) || err == nil && bot.IdentityGeneration != binding.BotGeneration {
		return display, nil
	}
	if err != nil {
		return display, err
	}
	destination, sourceID, err := destinationForAttempt(tx, scope, attempt, bot)
	if err != nil || destination.ChatID == "" {
		return display, err
	}
	rejection := ""
	switch event.TurnCompletionOrigin {
	case agentproto.TurnCompletionOriginThreadStartRejected, agentproto.TurnCompletionOriginThreadResumeRejected, agentproto.TurnCompletionOriginTurnStartRejected:
		rejection = event.CommandID
	}
	return workstore.PrivateExecutionDisplay{
		RejectionCommandID: rejection,
		ID:                 id, Binding: binding, Incarnation: carrier.Incarnation, SessionID: event.ThreadID, TurnID: event.TurnID,
		Tenant: destination.Tenant, ChatID: destination.ChatID, ReplyToMessageID: sourceID, Bot: bot,
		Progress: state.ExecCommandProgressRecord{ThreadID: event.ThreadID, TurnID: event.TurnID},
	}, nil
}

func executionVerbosity(tx *workstore.Tx, contextID string) (state.SurfaceVerbosity, error) {
	preferences, err := tx.GetContextPreferences(contextID)
	if err != nil && !errors.Is(err, workstore.ErrNotFound) {
		return "", err
	}
	if preferences.Verbose != nil {
		return state.NormalizeSurfaceVerbosity(state.SurfaceVerbosity(*preferences.Verbose)), nil
	}
	return state.SurfaceVerbosityNormal, nil
}

func destinationForAttempt(tx *workstore.Tx, scope work.WorkScope, attempt work.ExecutionAttempt, bot work.RoleBotBinding) (workstore.PersonalDestination, string, error) {
	for _, id := range attempt.InputIDs {
		input, err := tx.GetInbox(id)
		if err != nil {
			return workstore.PersonalDestination{}, "", err
		}
		if input.MessageID == "" || input.ScopeID != scope.ID || input.RecipientRoleID != bot.RoleID {
			continue
		}
		message, err := tx.GetMessage(input.MessageID)
		if err != nil {
			return workstore.PersonalDestination{}, "", err
		}
		if message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) || message.PlatformKey == nil {
			continue
		}
		for _, observation := range message.Observations {
			if observation.AppID == bot.AppID && observation.IdentityGeneration == bot.IdentityGeneration {
				m := message.PlatformKey
				return workstore.PersonalDestination{ChatID: m.ChatID, Tenant: m.Tenant}, m.MessageID, nil
			}
		}
	}
	dest, err := tx.GetPersonalContextDestination(scope.ID, attempt.ContextID, bot.RoleID)
	if errors.Is(err, workstore.ErrPersonalDestinationUnavailable) || errors.Is(err, workstore.ErrScopeDenied) {
		return dest, "", nil
	}
	return dest, "", err
}

func (p *projection) apply() error {
	event := p.event
	if event.Kind == agentproto.EventItemCompleted && event.ItemKind == "file_change" {
		p.collectFiles()
	}
	switch event.Kind {
	case agentproto.EventThreadTokenUsageUpdated:
		p.display.LatestTokenUsage = agentproto.CloneThreadTokenUsage(event.TokenUsage)
		return nil
	case agentproto.EventTurnStarted:
		if !p.display.StartedAt.IsZero() {
			return nil
		}
		p.display.StartedAt = p.now
		if p.display.Progress.Verbosity == state.SurfaceVerbosityQuiet {
			return nil
		}
		return p.progressStatus("已开始", "正在处理请求", "running")
	case agentproto.EventTurnCompleted:
		return p.finish()
	case agentproto.EventItemCompleted:
		if event.ItemKind == "agent_message" {
			return p.assistantMessage()
		}
	case agentproto.EventItemStarted:
		if event.ItemKind == "agent_message" {
			return nil
		}
	}
	if event.Kind != agentproto.EventSystemError && event.Kind != agentproto.EventRequestStarted &&
		(event.Kind != agentproto.EventItemCompleted || event.ItemKind != "image_generation") {
		if p.display.Progress.Verbosity == state.SurfaceVerbosityQuiet {
			return nil
		}
	}
	if p.continuesAfterPending(event) {
		if err := p.flushIntermediate(); err != nil {
			return err
		}
	}
	switch event.Kind {
	case agentproto.EventTurnPlanUpdated:
		return p.plan()
	case agentproto.EventTurnDiffUpdated:
		return p.turnDiff()
	case agentproto.EventSystemError:
		if event.Problem == nil || event.Problem.Severity == agentproto.ErrorSeverityWarning {
			return nil
		}
		return p.notice("执行遇到问题", failureMessage(event, "若本轮停止，可以在当前私聊重新发送需要继续的要求。"))
	case agentproto.EventRequestStarted:
		return nil // Workrequests owns the interactive question and its lifecycle.
	case agentproto.EventItemDelta, agentproto.EventItemStarted, agentproto.EventItemCompleted, agentproto.EventItemFileChangePatchUpdated, agentproto.EventItemReasoningSummaryPartAdded:
		return p.item()
	default:
		return nil
	}
}

func (p *projection) assistantMessage() error {
	event := p.event
	text := strings.TrimSpace(xutil.MetadataString(event.Metadata, "text"))
	if text == "" || event.ItemID == "" {
		return nil
	}
	if p.display.SeenMessages == nil {
		p.display.SeenMessages = make(map[string]bool)
	}
	if p.display.SeenMessages[event.ItemID] {
		return nil
	}
	p.display.SeenMessages[event.ItemID] = true
	if err := p.flushIntermediate(); err != nil {
		return err
	}
	phase := xutil.MetadataString(event.Metadata, "phase")
	if async := xutil.MetadataString(event.Metadata, "delivery") == "async"; phase == "commentary" || async {
		if p.display.Progress.Verbosity == state.SurfaceVerbosityQuiet && !async {
			return nil
		}
		return p.reply("阶段答复", text, nil, "")
	}
	if p.display.PendingFinal && p.display.Pending != "" {
		p.display.Pending += "\n\n" + text
		return nil
	}
	p.display.Pending, p.display.PendingItem, p.display.PendingFinal = text, event.ItemID, phase == "final_answer" || phase == "finalAnswer"
	return nil
}

func (p *projection) continuesAfterPending(event agentproto.Event) bool {
	switch event.Kind {
	case agentproto.EventItemStarted, agentproto.EventItemCompleted:
		return event.ItemID != "" && event.ItemID != p.display.PendingItem
	case agentproto.EventTurnPlanUpdated, agentproto.EventRequestStarted:
		return true
	default:
		return false
	}
}

func (p *projection) flushIntermediate() error {
	if p.display.Pending == "" || p.display.PendingFinal || p.display.Progress.Verbosity == state.SurfaceVerbosityQuiet {
		return nil
	}
	text := p.display.Pending
	p.display.Pending = ""
	return p.reply("阶段答复", text, nil, "")
}

func (p *projection) finish() error {
	text := p.display.Pending
	p.display.Pending = ""
	p.display.Terminated = true
	if p.display.ProgressAnchorID != "" {
		label, summary := "已完成", "本轮执行结束"
		switch p.event.Status {
		case "interrupted":
			label, summary = "已停止", "本轮执行已停止"
		case "failed":
			label, summary = "失败", "本轮未完成"
		}
		if err := p.progressStatus(label, summary, p.event.Status); err != nil {
			return err
		}
	}
	switch p.event.Status {
	case "interrupted":
		return p.notice("已停止", "本轮执行已停止。可以直接发送新消息开始下一轮。")
	case "failed":
		if text != "" {
			if err := p.finalReply(text); err != nil {
				return err
			}
		}
		return p.notice("执行失败", failureMessage(p.event, "已收到的消息与中间结果仍然保留，可以继续发送要求。"))
	}
	if text == "" {
		if p.display.HadOutput {
			if len(p.display.SeenMessages) != 0 {
				return p.notice("执行已结束", "本轮没有返回最终答复。已返回的阶段内容仍然保留。")
			}
			return nil
		}
		return p.notice("执行已结束", "本轮没有返回可显示的答复。")
	}
	return p.finalReply(text)
}

func failureMessage(event agentproto.Event, next string) string {
	message := strings.TrimSpace(event.ErrorMessage)
	if problem := event.Problem; problem != nil && strings.TrimSpace(problem.Message) != "" {
		message = strings.TrimSpace(problem.Message)
	}
	if message, _, _ := strings.Cut(message, "\n"); message != "" {
		return xutil.TruncateRunes(message, 700, xutil.TruncateOptions{}) + "\n" + next
	}
	return "本轮未完成。" + next
}

func (p *projection) notice(title, message string) error {
	_, err := p.enqueue(feishu.PrivateExecutionNotice(title, message), nil, "", "", "")
	return err
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return parts[0] + "-" + hex.EncodeToString(sum[:16])
}

func (p *projection) outputID() string {
	p.ordinal++
	if p.event.Journal == nil {
		return fmt.Sprintf("private-display-live-%020d-%05d-%s", p.display.Revision, p.ordinal, p.display.ID)
	}
	return fmt.Sprintf("private-display-%020d-%05d-%s", p.event.Journal.Sequence, p.ordinal, p.display.ID)
}
