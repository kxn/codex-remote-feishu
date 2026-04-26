package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
	requestID := t.nextRequest("review-start")
	t.pendingReviewStart[requestID] = pendingReviewStart{
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

func (t *Translator) applyPendingReviewThread(record *agentproto.ThreadSnapshotRecord) {
	if record == nil || strings.TrimSpace(record.ThreadID) == "" {
		return
	}
	pending, ok := t.pendingReviewThreads[record.ThreadID]
	if !ok {
		return
	}
	if record.Source == nil {
		record.Source = &agentproto.ThreadSourceRecord{
			Kind: agentproto.ThreadSourceKindReview,
			Name: "review",
		}
	}
	if strings.TrimSpace(record.ForkedFromID) == "" {
		record.ForkedFromID = strings.TrimSpace(pending.ParentThreadID)
	}
	if record.Source != nil && strings.TrimSpace(record.Source.ParentThreadID) == "" {
		record.Source.ParentThreadID = strings.TrimSpace(pending.ParentThreadID)
	}
}
