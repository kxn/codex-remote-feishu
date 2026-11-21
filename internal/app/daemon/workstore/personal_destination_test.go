package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestPersonalDestinationSurvivesSystemInputsAndRejectsOtherAppGeneration(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "private", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, 0); err != nil {
			return err
		}
		for _, id := range []string{"original-context", "other-context"} {
			if err := tx.PutContext(work.WorkContext{ID: id, ScopeID: "private", RoleID: "r", Workstream: id, Revision: 1}, 0); err != nil {
				return err
			}
		}
		if err := tx.PutContent(work.NewContent("private-body", "private", "text/plain", []byte("hello"), time.Now().UTC())); err != nil {
			return err
		}
		for _, v := range []struct {
			id, context, chat string
			actor             work.Actor
		}{
			{"human", "original-context", "original-chat", work.Actor{Kind: work.ActorPrincipal, ID: "p"}},
			{"system", "original-context", "", work.Actor{Kind: work.ActorSystem, ID: "recovery"}},
			{"other", "other-context", "other-chat", work.Actor{Kind: work.ActorPrincipal, ID: "p"}},
		} {
			m := work.Message{ID: v.id, ScopeID: "private", ConversationID: v.context, Source: v.actor, BodyRef: "private-body", Revision: 1, CreatedAt: time.Now().UTC()}
			if v.chat != "" {
				m.PlatformKey = &work.MessageKey{Tenant: "tenant", ChatID: v.chat, MessageID: v.id}
				m.Observations = []work.Observation{{AppID: "app", IdentityGeneration: 1}}
			}
			input := testInput("input-"+v.id, "r", v.context)
			input.ScopeID = "private"
			if _, _, err := tx.AcceptMessage(m, []work.Inbox{input}); err != nil {
				return err
			}
		}
		dest, err := tx.GetPersonalContextDestination("private", "original-context", "r")
		if err != nil {
			return err
		}
		if dest.ChatID != "original-chat" {
			t.Fatalf("retargeted to %+v", dest)
		}
		if err = tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "replacement-app", IdentityGeneration: 2}, 1); err != nil {
			return err
		}
		if _, err = tx.GetPersonalContextDestination("private", "original-context", "r"); err == nil {
			t.Fatal("old app private destination reused")
		}
		return nil
	})
}
