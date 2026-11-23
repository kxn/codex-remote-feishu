package workingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workdisplay"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type privateImageExpander struct{}

func (privateImageExpander) Expand(_ context.Context, raw work.RawIngressEnvelope) (ExpandedMessage, error) {
	count := 1
	text := "[已读取图片附件]"
	if raw.MessageType == "post" {
		count = 2
		text = "Explain these two pictures"
	}
	result := ExpandedMessage{Text: text, Complete: true}
	for i := 0; i < count; i++ {
		result.Attachments = append(result.Attachments, ExpandedAttachment{Kind: "image", MediaType: "image/png", SourceMessageID: raw.MessageID, Data: []byte{137, 80, 78, 71, byte(i)}})
	}
	return result, nil
}

func TestPrivateMediaStagingPersistsDeduplicatesAndRecallCancels(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	ingress.config.Expander = privateImageExpander{}
	raw := rawMessage("leader", "private-image", "private-chat", "p2p", "open", "user", "")
	raw.MessageType = "image"
	raw.RawContent = `{"image_key":"opaque"}`
	first := receive(t, ingress, raw)
	raw.EventID = "redelivered-image"
	second := receive(t, ingress, raw)
	if first.IngressID != second.IngressID {
		t.Fatal("SDK redelivery created second image input")
	}
	if result := process(t, ingress); result.Processed != 1 {
		t.Fatalf("image processing: %+v", result)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		record, e := tx.GetIngress(first.IngressID)
		if e != nil {
			return e
		}
		message, e := tx.GetMessage(record.MessageID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListInbox(message.ScopeID)
		if e != nil {
			return e
		}
		if len(inputs) != 1 || !inputs[0].AwaitingText || inputs[0].State != work.InboxPending || len(message.Attachments) != 1 {
			t.Fatal("private image lost durable staged input")
		}
		asset := message.Attachments[0]
		if _, e = tx.GetContent(message.ScopeID, asset.ContentRef); e != nil {
			return e
		}
		_, _, e = tx.AcceptPlatformObservation(workstore.PlatformObservation{Gateway: record.Gateway, EventID: "image-recall", Tenant: raw.Tenant, ChatID: raw.ChatID, MessageID: raw.MessageID, Kind: "message.recalled", Facts: json.RawMessage(`{"source":"sdk-recall"}`), ReceivedAt: time.Now()})
		if e != nil {
			return e
		}
		input, e := tx.GetInbox(inputs[0].ID)
		if e != nil {
			return e
		}
		if input.State != work.InboxCancelled || !input.AwaitingText {
			t.Fatal("recall did not cancel only the unclaimed staged image")
		}
		if _, e = tx.GetContent(message.ScopeID, asset.ContentRef); e != nil {
			t.Fatal("recall erased immutable image source")
		}
		return nil
	})
}

func TestPrivateCaptionedPostKeepsBothActualImagesAndRunsWithItsOwnText(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	ingress.config.Expander = privateImageExpander{}
	raw := rawMessage("leader", "captioned-post", "private-chat", "p2p", "open", "user", "")
	raw.MessageType = "post"
	raw.RawContent = `{"zh_cn":{"title":"","content":[[{"tag":"text","text":"Explain these two pictures"},{"tag":"img","image_key":"one"},{"tag":"img","image_key":"two"}]]}}`
	receipt := receive(t, ingress, raw)
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		record, e := tx.GetIngress(receipt.IngressID)
		if e != nil {
			return e
		}
		message, e := tx.GetMessage(record.MessageID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListInbox(message.ScopeID)
		if e != nil {
			return e
		}
		if len(inputs) != 1 || inputs[0].AwaitingText || len(message.Attachments) != 2 {
			t.Fatal("captioned post was split or incorrectly staged")
		}
		return nil
	})
}

func TestPrivateFailedImageShowsNoticeAndRemainsStagedInsteadOfLaunchingModel(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	ingress.config.Expander = failedExpander{}
	ingress.config.QueuePrivateExecution = workdisplay.QueuePrivateInputFeedback
	raw := rawMessage("leader", "failed-private-image", "private-chat", "p2p", "open", "user", "")
	raw.MessageType = "image"
	raw.RawContent = `{"image_key":"opaque-failed"}`
	receipt := receive(t, ingress, raw)
	result := process(t, ingress)
	if result.Partial != 1 {
		t.Fatal("download failure lost partial source")
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		record, e := tx.GetIngress(receipt.IngressID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListInbox(record.Route.ScopeID)
		if e != nil {
			return e
		}
		if len(inputs) != 1 || !inputs[0].AwaitingText || inputs[0].ContentState != work.InputContentPartial {
			t.Fatal("failed image became automatic model instruction")
		}
		notices, e := tx.ListOutbox(record.Route.ScopeID)
		if e != nil {
			return e
		}
		if len(notices) != 1 {
			t.Fatal("image failure has no user-visible notice")
		}
		body, e := tx.GetContent(record.Route.ScopeID, notices[0].PayloadRef)
		if e != nil {
			return e
		}
		var projection workservice.MessageProjection
		if e = json.Unmarshal(body.Data, &projection); e != nil {
			return e
		}
		if !strings.Contains(string(projection.Card), "尚未启动助手") || strings.Contains(string(projection.Card), "opaque-failed") {
			t.Fatal("failure notice is missing or exposes platform resource keys")
		}
		return nil
	})
}

func TestPrivateMediaOverBudgetShowsRecoveryNoticeAndRecallMakesBatchRunnable(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	ingress.config.Expander = privateImageExpander{}
	ingress.config.QueuePrivateExecution = workdisplay.QueuePrivateInputFeedback
	var first work.RawIngressEnvelope
	var firstReceipt ReceiveReceipt
	for i := 0; i < 65; i++ {
		raw := rawMessage("leader", fmt.Sprintf("many-images-%02d", i), "private-chat", "p2p", "open", "user", "")
		raw.MessageType = "image"
		raw.RawContent = `{"image_key":"opaque"}`
		receipt := receive(t, ingress, raw)
		if i == 0 {
			first, firstReceipt = raw, receipt
		}
	}
	result := process(t, ingress)
	if result.Processed != 65 {
		t.Fatalf("media budget discarded source inputs: %+v", result)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		record, e := tx.GetIngress(firstReceipt.IngressID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListPendingInputs(record.Route.ScopeID, record.Gateway.RoleID)
		if e != nil {
			return e
		}
		if len(inputs) != 65 {
			t.Fatal("excess media was silently dropped")
		}
		within, e := tx.MediaBatchWithinBudget(inputs)
		if e != nil {
			return e
		}
		if within {
			t.Fatal("native attachment bound ignored")
		}
		outbox, e := tx.ListOutbox(record.Route.ScopeID)
		if e != nil {
			return e
		}
		found := false
		for _, notice := range outbox {
			body, e := tx.GetContent(record.Route.ScopeID, notice.PayloadRef)
			if e != nil {
				return e
			}
			if strings.Contains(string(body.Data), "64MiB") && strings.Contains(string(body.Data), "/stop") {
				found = true
			}
		}
		if !found {
			t.Fatal("over-budget private queue has no concrete recovery notice")
		}
		_, _, e = tx.AcceptPlatformObservation(workstore.PlatformObservation{Gateway: record.Gateway, EventID: "reduce-media-batch", Tenant: first.Tenant, ChatID: first.ChatID, MessageID: first.MessageID, Kind: "message.recalled", Facts: json.RawMessage(`{"source":"sdk-recall"}`), ReceivedAt: time.Now()})
		if e != nil {
			return e
		}
		remaining, e := tx.ListPendingInputs(record.Route.ScopeID, record.Gateway.RoleID)
		if e != nil {
			return e
		}
		within, e = tx.MediaBatchWithinBudget(remaining)
		if e != nil {
			return e
		}
		if len(remaining) != 64 || !within {
			t.Fatal("recall did not restore recoverable media budget")
		}
		return nil
	})
}
