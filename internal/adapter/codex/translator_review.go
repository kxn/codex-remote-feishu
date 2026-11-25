package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/jsonrpcutil"
)

func reviewStartInitiator(command agentproto.Command) agentproto.Initiator {
	surfaceID := strings.TrimSpace(choose(command.Origin.Surface, command.Origin.ChatID))
	if surfaceID == "" {
		return agentproto.Initiator{Kind: agentproto.InitiatorUnknown}
	}
	return agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surfaceID}
}

func buildReviewTarget(target agentproto.ReviewTarget) (map[string]any, error) {
	target = target.Normalized()
	if !target.Valid() {
		return nil, fmt.Errorf("review.start requires a valid target")
	}
	switch target.Kind {
	case agentproto.ReviewTargetKindUncommittedChanges:
		return map[string]any{"type": "uncommittedChanges"}, nil
	case agentproto.ReviewTargetKindBaseBranch:
		return map[string]any{"type": "baseBranch", "branch": target.Branch}, nil
	case agentproto.ReviewTargetKindCommit:
		payload := map[string]any{"type": "commit", "sha": target.CommitSHA}
		if target.CommitTitle != "" {
			payload["title"] = target.CommitTitle
		}
		return payload, nil
	case agentproto.ReviewTargetKindCustom:
		return map[string]any{"type": "custom", "instructions": target.Instructions}, nil
	default:
		return nil, fmt.Errorf("unsupported review target kind %q", target.Kind)
	}
}

func (t *Translator) translateReviewStart(command agentproto.Command) ([][]byte, error) {
	threadID := strings.TrimSpace(command.Target.ThreadID)
	if threadID == "" {
		return nil, fmt.Errorf("review.start requires thread id")
	}
	review := command.Review.Normalized()
	target, err := buildReviewTarget(review.Target)
	if err != nil {
		return nil, err
	}
	if review.Delivery == agentproto.ReviewDeliveryDetached {
		if strings.TrimSpace(command.Target.CWD) == "" {
			return nil, fmt.Errorf("detached review requires frozen working directory")
		}
		requestID := t.NextRequest("review-thread-start")
		t.pendingReviewThreadCreate[requestID] = pendingReviewThreadCreate{Command: command, PreviousThreadID: t.currentThreadID}
		params := t.buildThreadStartParams(command.Target.CWD, command.Overrides, command.CodexResume, false)
		params["ephemeral"] = true
		payload, err := json.Marshal(map[string]any{"id": requestID, "method": "thread/start", "params": params})
		if err != nil {
			delete(t.pendingReviewThreadCreate, requestID)
			return nil, err
		}
		return [][]byte{append(payload, '\n')}, nil
	}
	requestID := t.NextRequest("review-start")
	t.pendingReviewStart[requestID] = pendingReviewStart{
		CommandID: command.CommandID, PrivateNative: command.NativeBinding != nil,
		ThreadID:  threadID,
		Initiator: reviewStartInitiator(command),
	}
	params := map[string]any{
		"threadId": threadID,
		"target":   target,
	}
	if delivery := agentproto.NormalizeReviewDelivery(review.Delivery); delivery != "" {
		params["delivery"] = string(delivery)
	}
	payload := map[string]any{
		"id":     requestID,
		"method": "review/start",
		"params": params,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return [][]byte{append(bytes, '\n')}, nil
}

// A detached product review uses a dedicated native thread; modern Codex only
// supports inline review on the supplied thread. The start response is the
// sole authority connecting the new thread to the original command.
func (t *Translator) reviewThreadCreated(requestID string, pending pendingReviewThreadCreate, message map[string]any) (Result, error) {
	command := pending.Command
	parent := strings.TrimSpace(command.Target.ThreadID)
	initiator := reviewStartInitiator(command)
	if problem := jsonrpcutil.ExtractErrorMessage(message); problem != "" {
		if command.NativeBinding != nil {
			return Result{Suppress: true, Events: []agentproto.Event{{Kind: agentproto.EventNativeControlResult, CommandID: command.CommandID, RequestID: requestID, ThreadID: parent, Action: "review.start", Status: "failed", ErrorMessage: problem, Metadata: map[string]any{"stage": "thread/start"}}}}, nil
		}
		return Result{Suppress: true, Events: []agentproto.Event{agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{CommandID: command.CommandID, RequestID: requestID, ThreadID: parent, SurfaceSessionID: initiator.SurfaceSessionID, Code: "review_start_failed", Operation: "review.start", Stage: "thread/start", Message: "Codex 无法创建审阅会话。", Details: problem})}}, nil
	}
	child := strings.TrimSpace(lookupString(message, "result", "thread", "id"))
	if child == "" || child == parent {
		if command.NativeBinding == nil {
			return Result{Suppress: true, Events: []agentproto.Event{agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{CommandID: command.CommandID, RequestID: requestID, ThreadID: parent, SurfaceSessionID: initiator.SurfaceSessionID, Code: "review_start_unconfirmed", Operation: "review.start", Stage: "thread/start", Message: "无法确认独立审阅会话，未自动重试。"})}}, nil
		}
		return Result{Suppress: true, Events: []agentproto.Event{{Kind: agentproto.EventNativeControlResult, CommandID: command.CommandID, RequestID: requestID, ThreadID: parent, Action: "review.start", Status: "unknown", Metadata: map[string]any{"stage": "thread/start"}}}}, nil
	}
	if t.currentThreadID == child {
		t.currentThreadID = pending.PreviousThreadID
	}
	t.knownThreadCWD[child] = strings.TrimSpace(command.Target.CWD)
	if model := strings.TrimSpace(lookupString(message, "result", "model")); model != "" {
		t.knownThreadModel[child] = model
	}
	t.pendingReviewThreads[child] = pendingReviewThread{ParentThreadID: parent, Initiator: initiator, Independent: true}
	if initiator.Kind == agentproto.InitiatorRemoteSurface {
		t.pendingRemoteTurnByThread[child] = initiator.SurfaceSessionID
	}
	target, err := buildReviewTarget(command.Review.Target)
	if err != nil {
		return Result{}, err
	}
	next := t.NextRequest("review-start")
	t.pendingReviewStart[next] = pendingReviewStart{CommandID: command.CommandID, PrivateNative: command.NativeBinding != nil, ThreadID: parent, ReviewThreadID: child, Initiator: initiator, Independent: true}
	frame, err := json.Marshal(map[string]any{"id": next, "method": "review/start", "params": map[string]any{"threadId": child, "target": target, "delivery": "inline"}})
	if err != nil {
		delete(t.pendingReviewStart, next)
		return Result{}, err
	}
	result := Result{Suppress: true, OutboundToCodex: [][]byte{append(frame, '\n')}}
	if command.NativeBinding != nil {
		result.Events = []agentproto.Event{{Kind: agentproto.EventReviewChildCreated, CommandID: command.CommandID, RequestID: requestID, ThreadID: child, Action: "review.start", Metadata: map[string]any{"parentThreadID": parent, "stage": "thread/start"}}}
	}
	return result, nil
}

func (t *Translator) applyPendingReviewThread(record *agentproto.ThreadSnapshotRecord) {
	if record == nil || strings.TrimSpace(record.ThreadID) == "" {
		return
	}
	pending, ok := t.pendingReviewThreads[record.ThreadID]
	if !ok {
		return
	}
	// This exact thread ID came from the journaled review thread/start
	// response. Codex may label a client-started thread "vscode"; that
	// native default must not override the application's proven review owner.
	record.Source = &agentproto.ThreadSourceRecord{
		Kind: agentproto.ThreadSourceKindReview,
		Name: "review",
	}
	if !pending.Independent && strings.TrimSpace(record.ForkedFromID) == "" {
		record.ForkedFromID = strings.TrimSpace(pending.ParentThreadID)
	}
	if record.Source != nil && strings.TrimSpace(record.Source.ParentThreadID) == "" {
		record.Source.ParentThreadID = strings.TrimSpace(pending.ParentThreadID)
	}
}

func pendingReviewThreadDiscoveredEvent(reviewThreadID, turnID string, pending pendingReviewStart) (agentproto.Event, bool) {
	reviewThreadID = strings.TrimSpace(reviewThreadID)
	parentThreadID := strings.TrimSpace(pending.ThreadID)
	if reviewThreadID == "" || parentThreadID == "" {
		return agentproto.Event{}, false
	}
	metadata := map[string]any{
		"threadSource": &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindReview, Name: "review", ParentThreadID: parentThreadID},
	}
	if !pending.Independent {
		metadata["forkedFromId"] = parentThreadID
	}
	return agentproto.Event{
		Kind:      agentproto.EventThreadDiscovered,
		ThreadID:  reviewThreadID,
		TurnID:    strings.TrimSpace(turnID),
		Initiator: pending.Initiator,
		Metadata:  metadata,
	}, true
}
