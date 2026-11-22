package feishu

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestProjectPublishUsesDurableUUIDAndActualReceipt(t *testing.T) {
	for _, reply := range []string{"", "om_original"} {
		t.Run(reply, func(t *testing.T) {
			calls := 0
			api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				body := projectReadJSON(t, r)
				if r.Method != "POST" || body["uuid"] != "fixed-uuid" || body["msg_type"] != "text" {
					t.Fatalf("request %s %+v", r.Method, body)
				}
				if reply != "" && body["reply_in_thread"] != true {
					t.Fatal("reply did not preserve topic")
				}
				fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_sent","chat_id":"oc_project"}}`)
			})
			receipt, err := api.PublishText(context.Background(), ProjectPublishRequest{ChatID: "oc_project", ReplyToMessageID: reply, ReplyInThread: true, Text: "Work is waiting for your decision", UUID: "fixed-uuid"})
			if err != nil || receipt.MessageID != "om_sent" || calls != 1 {
				t.Fatalf("receipt=%+v calls=%d err=%v", receipt, calls, err)
			}
		})
	}
}

func TestProjectPublishDoesNotRetryOrInventDelivery(t *testing.T) {
	calls := 0
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(429)
		fmt.Fprint(w, `{"code":99991400,"msg":"too many requests"}`)
	})
	if _, err := api.PublishText(context.Background(), ProjectPublishRequest{ChatID: "oc_project", Text: "Summary", UUID: "fixed-uuid"}); err == nil {
		t.Fatal("rate limit claimed delivery")
	}
	if calls != 1 {
		t.Fatalf("retried write %d times", calls)
	}
}
