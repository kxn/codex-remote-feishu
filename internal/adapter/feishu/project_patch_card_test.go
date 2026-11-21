package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestProjectCardPatchPreservesExactMessageAndNeverRetries(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			calls := 0
			api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				body := projectReadJSON(t, r)
				if r.Method != "PATCH" || r.URL.Path != "/open-apis/im/v1/messages/om_fixed" || body["content"] != `{"schema":"2.0"}` {
					t.Fatalf("unexpected patch %s %s %+v", r.Method, r.URL.Path, body)
				}
				if fail {
					w.WriteHeader(503)
					fmt.Fprint(w, `{"code":999,"msg":"unavailable"}`)
					return
				}
				fmt.Fprint(w, `{"code":0}`)
			})
			receipt, err := api.PatchCard(context.Background(), ProjectCardPatchRequest{ChatID: "oc_project", MessageID: "om_fixed", Card: json.RawMessage(`{"schema":"2.0"}`)})
			if calls != 1 || (err != nil) != fail {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if !fail && (receipt.MessageID != "om_fixed" || receipt.ChatID != "oc_project") {
				t.Fatal(receipt)
			}
		})
	}
}
