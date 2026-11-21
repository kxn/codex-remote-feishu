package workingress

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

type durableAttachmentExpander struct{}

func (durableAttachmentExpander) Expand(context.Context, work.RawIngressEnvelope) (ExpandedMessage, error) {
	panic("must use accepted identity")
}
func (durableAttachmentExpander) ExpandAccepted(_ context.Context, record work.IngressRecord) (ExpandedMessage, error) {
	return ExpandedMessage{Text: "actual attachment", Complete: true, Attachments: []ExpandedAttachment{{Kind: "file", MediaType: "text/plain", FileName: "notes.txt", SourceMessageID: record.Envelope.MessageID, Data: []byte("actual durable file bytes")}}}, nil
}
func TestIngressPersistsActualAttachmentBytesInMessageScope(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	ingress.config.Expander = durableAttachmentExpander{}
	receipt := receive(t, ingress, rawMessage("leader", "attachment", "group", "group", "open", "user", ""))
	result := process(t, ingress)
	if result.Processed != 1 || result.Partial != 0 {
		t.Fatalf("process %+v", result)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		record, err := tx.GetIngress(receipt.IngressID)
		if err != nil {
			return err
		}
		message, err := tx.GetMessage(record.MessageID)
		if err != nil {
			return err
		}
		if len(message.Attachments) != 1 {
			t.Fatalf("lost attachment %+v", message)
		}
		attachment := message.Attachments[0]
		content, err := tx.GetContent(message.ScopeID, attachment.ContentRef)
		if err != nil {
			return err
		}
		if string(content.Data) != "actual durable file bytes" || content.SHA256 != attachment.SHA256 {
			t.Fatal("opaque key substituted for bytes")
		}
		if _, err = tx.GetContent("foreign", attachment.ContentRef); err == nil {
			t.Fatal("crossscope attachment readable")
		}
		return nil
	})
}
