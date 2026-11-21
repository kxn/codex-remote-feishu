package feishu

import (
	"fmt"
	"net/http"
	"testing"
)

func TestPrivateEntryUsesActualOpenIDAndReceipt(t *testing.T) {
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("receive_id_type") != "open_id" {
			t.Errorf("invented chat identity %s", r.URL.RawQuery)
		}
		body := projectReadJSON(t, r)
		if body["receive_id"] != "ou_person" || body["uuid"] != "fixed-uuid" || body["msg_type"] != "text" {
			t.Errorf("request %#v", body)
		}
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"message_id":"actual-message","chat_id":"actual-private-chat"}}`)
	})
	got, err := api.PublishPrivateEntry(t.Context(), PrivateEntryPublishRequest{OpenID: "ou_person", Text: "Private workspace", UUID: "fixed-uuid"})
	if err != nil || got.ChatID != "actual-private-chat" || got.MessageID != "actual-message" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestPrivateEntryWriteFailureIsNotRetried(t *testing.T) {
	calls := 0
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
		_, _ = fmt.Fprint(w, `{"code":999,"msg":"failed"}`)
	})
	if _, err := api.PublishPrivateEntry(t.Context(), PrivateEntryPublishRequest{OpenID: "ou_person", Text: "Private workspace", UUID: "fixed-uuid"}); err == nil {
		t.Fatal("write failure swallowed")
	}
	if calls != 1 {
		t.Fatalf("write retried%d", calls)
	}
}
