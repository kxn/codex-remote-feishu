package feishu

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestPrivateReplyDoesNotCreateThreadForTextCardOrFile(t *testing.T) {
	for _, kind := range []string{"text", "card", "file"} {
		t.Run(kind, func(t *testing.T) {
			api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
				body := projectReadJSON(t, r)
				if body["reply_in_thread"] != false {
					t.Fatalf("private %s opened a topic: %+v", kind, body)
				}
				if r.URL.Path != "/open-apis/im/v1/messages/om_user/reply" {
					t.Fatalf("reply attribution lost: %s", r.URL.Path)
				}
				fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_reply","chat_id":"oc_private"}}`)
			})
			var err error
			switch kind {
			case "text":
				_, err = api.PublishText(context.Background(), ProjectPublishRequest{ChatID: "oc_private", ReplyToMessageID: "om_user", Text: "Answer", UUID: "stable"})
			case "card":
				_, err = api.PublishCard(context.Background(), ProjectCardPublishRequest{ChatID: "oc_private", ReplyToMessageID: "om_user", Card: []byte(`{"schema":"2.0","body":{"elements":[]}}`), UUID: "stable"})
			case "file":
				_, err = api.PublishResultFile(context.Background(), PublishResultFileRequest{ChatID: "oc_private", ReplyToMessageID: "om_user", FileKey: "file_key", UUID: "stable"})
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
