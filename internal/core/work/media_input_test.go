package work

import (
	"testing"
	"time"
)

func TestStandaloneMediaWaitsForActualInstruction(t *testing.T) {
	for _, tc := range []struct {
		kind, raw string
		waiting   bool
	}{
		{"image", `{"image_key":"opaque"}`, true}, {"file", `{"file_key":"opaque"}`, true},
		{"post", `{"zh_cn":{"title":"","content":[[{"tag":"img","image_key":"a"},{"tag":"img","image_key":"b"}]]}}`, true},
		{"post", `{"content":[[{"tag":"text","text":"look at this"},{"tag":"img","image_key":"a"}]]}`, false},
		{"post", `{"title":"analyze","content":[[{"tag":"img","image_key":"a"}]]}`, false},
		{"post", `invalid`, true}, {"text", `{"text":"hello"}`, false},
	} {
		if got := StandaloneMedia(RawIngressEnvelope{MessageType: tc.kind, RawContent: tc.raw}); got != tc.waiting {
			t.Fatalf("kind=%s waiting=%v want=%v", tc.kind, got, tc.waiting)
		}
	}
}
func TestMediaAdmissionNeedsLaterHumanMessageInSameContext(t *testing.T) {
	now := time.Now()
	image := Inbox{ID: "image", ScopeID: "private", RecipientContextID: "front", MessageID: "image-message", AwaitingText: true, AvailableAt: now}
	text := Inbox{ID: "text", ScopeID: "private", RecipientContextID: "front", MessageID: "text-message", AvailableAt: now.Add(time.Second)}
	if MediaBatchHasInstruction([]Inbox{image}) || !MediaBatchHasInstruction([]Inbox{image, text}) {
		t.Fatal("standalone image or companion text ownership lost")
	}
	for _, mutate := range []func(*Inbox){func(i *Inbox) { i.AvailableAt = now.Add(-time.Second) }, func(i *Inbox) { i.RecipientContextID = "another" }, func(i *Inbox) { i.ScopeID = "another" }, func(i *Inbox) { i.ControlOfInputID = "internal" }, func(i *Inbox) { i.MessageID = "" }} {
		other := text
		mutate(&other)
		if MediaBatchHasInstruction([]Inbox{image, other}) {
			t.Fatal("unrelated or earlier input triggered image")
		}
	}
}
