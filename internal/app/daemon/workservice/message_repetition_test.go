package workservice

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestRepeatedRoleMessagesProduceOneCoordinationNoticeUntilNewHumanInput(t *testing.T) {
	s, token, run := runningService(t)
	var blocked string
	for i := 0; i < 6; i++ {
		result, err := s.SendMessage(t.Context(), token, run, SendMessageRequest{Key: fmt.Sprintf("key-%d", i), RecipientRoleID: "leader", ConversationID: "subject", Body: "Please report the same progress"})
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			if result.Status != "completed" {
				t.Fatalf("first delivery suppressed: %#v", result)
			}
		} else {
			if result.Status != "needs_coordination" {
				t.Fatalf("loop continues: %#v", result)
			}
			if blocked != "" && result.ObjectID != blocked {
				t.Fatal("duplicate blocker")
			}
			blocked = result.ObjectID
		}
	}
	err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		inputs, e := tx.ListInbox("scope")
		if e != nil {
			return e
		}
		if len(inputs) != 3 {
			t.Fatalf("want two original deliveries and one coordination notice, got %d", len(inputs))
		}
		body := work.NewContent("fresh-human-body", "scope", "text/plain", []byte("Please ask again; circumstances changed"), s.now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.AcceptMessage(work.Message{ID: "fresh-human", ScopeID: "scope", ConversationID: "subject", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, BodyRef: body.ID, Revision: 1, CreatedAt: s.now()}, nil)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SendMessage(t.Context(), token, run, SendMessageRequest{Key: "after-human", RecipientRoleID: "leader", ConversationID: "subject", Body: "Please report the same progress"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" {
		t.Fatal("new human instruction failed to reset episode")
	}
	result, err = s.SendMessage(t.Context(), token, run, SendMessageRequest{Key: "new-evidence", RecipientRoleID: "leader", ConversationID: "subject", Body: "The build finished; here is new evidence"})
	if err != nil || result.Status != "completed" {
		t.Fatalf("different substantive message blocked: %#v %v", result, err)
	}
	// Exact key replay still returns the original block and creates no new wake.
	result, err = s.SendMessage(t.Context(), token, run, SendMessageRequest{Key: "key-5", RecipientRoleID: "leader", ConversationID: "subject", Body: "Please report the same progress"})
	if err != nil || !result.Replay || result.ObjectID != blocked || result.Status != "needs_coordination" {
		t.Fatalf("replay changed outcome: %#v %v", result, err)
	}
}
