package workruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func reviewSource(tx *workstore.Tx, binding agentproto.ExecutionBinding, event agentproto.Event) (work.SourceRef, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return work.SourceRef{}, err
	}
	id := fmt.Sprintf("native-review:%s:%d", event.Journal.CarrierIncarnation, event.Journal.Sequence)
	content := work.NewContent(id, binding.ScopeID, "application/json", raw, time.Now().UTC())
	if err = tx.PutContent(content); err != nil {
		return work.SourceRef{}, err
	}
	return work.SourceRef{Kind: string(event.Kind), ID: id, Version: event.Journal.PayloadHash, EvidenceRef: id}, nil
}

func recordReviewNativeEvent(tx *workstore.Tx, carrier workstore.CarrierRegistration, binding agentproto.ExecutionBinding, event agentproto.Event) error {
	if event.Kind != agentproto.EventTurnCompleted || event.Status != "completed" {
		return nil
	}
	if event.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime {
		return nil
	}
	events, err := tx.NativeFinalMessageEvents(carrier.Incarnation, event.ThreadID, event.TurnID, event.Journal.Sequence)
	if err != nil {
		return err
	}
	var sources []work.SourceRef
	var text strings.Builder
	seen := map[string]string{}
	for _, message := range events {
		original, ok := message.Metadata["text"].(string)
		if !ok || original == "" || message.ItemID == "" {
			continue
		}
		if previous, exists := seen[message.ItemID]; exists {
			if previous != original {
				return fmt.Errorf("native review item changed after completion")
			}
			continue
		}
		seen[message.ItemID] = original
		if text.Len()+len(original) > 512*1024 {
			return fmt.Errorf("review result exceeds its bounded native evidence limit")
		}
		source, e := reviewSource(tx, binding, message)
		if e != nil {
			return e
		}
		sources = append(sources, source)
		text.WriteString(original)
	}
	if len(sources) == 0 {
		return nil
	} // The normal missing-report recovery path retains the request.
	terminal, err := reviewSource(tx, binding, event)
	if err != nil {
		return err
	}
	_, _, err = tx.RecordReviewResult(binding.AttemptID, terminal, text.String(), sources)
	return err
}
