package feishu

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func teamExpansionGateway(t *testing.T, handler http.HandlerFunc) *LiveGateway {
	fake := projectChatTestAPI(t, handler)
	g := NewLiveGateway(LiveGatewayConfig{GatewayID: "project-test", AppID: "app"})
	g.broker = fake.broker
	return g
}
func TestTeamExpansionDownloadsImageAndQuotesActualBoundMessage(t *testing.T) {
	calls := 0
	png := append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, make([]byte, 32)...)
	g := teamExpansionGateway(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/open-apis/im/v1/messages/message/resources/image-key":
			if r.URL.Query().Get("type") != "image" {
				t.Errorf("download query %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		case "/open-apis/im/v1/messages/quoted":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"quoted","chat_id":"group","msg_type":"text","body":{"content":"{\"text\":\"actual quoted body\"}"}}]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	event := durableIngressEvent()
	event.Event.Message.MessageType = stringRef("image")
	event.Event.Message.Content = stringRef(`{"image_key":"image-key"}`)
	event.Event.Message.ParentId = stringRef("quoted")
	raw, _, err := g.rawMessageEnvelope(event)
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.ExpandTeamMessage(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete || len(got.Attachments) != 1 || !bytes.Equal(got.Attachments[0].Data, png) || got.Attachments[0].MediaType != "image/png" || !strings.Contains(got.Text, "actual quoted body") || calls != 2 {
		t.Fatalf("expansion %#v calls%d", got, calls)
	}
}
func TestTeamExpansionRejectsCrossChatQuoteAndOversizeAssets(t *testing.T) {
	g := teamExpansionGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/quoted") {
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"quoted","chat_id":"foreign","msg_type":"text","body":{"content":"{\"text\":\"PRIVATE_SECRET\"}"}}]}}`)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(bytes.Repeat([]byte("x"), teamAssetLimit+1))
	})
	event := durableIngressEvent()
	event.Event.Message.MessageType = stringRef("file")
	event.Event.Message.Content = stringRef(`{"file_key":"file-key","file_name":"../untrusted.txt"}`)
	event.Event.Message.ParentId = stringRef("quoted")
	raw, _, _ := g.rawMessageEnvelope(event)
	got, err := g.ExpandTeamMessage(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete || len(got.Attachments) != 0 || len(got.Issues) != 2 || strings.Contains(got.Text, "PRIVATE_SECRET") {
		t.Fatalf("unsafe expansion %+v", got)
	}
}
func TestTeamExpansionForwardTreeIsBoundedAndPreservesChildren(t *testing.T) {
	g := teamExpansionGateway(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"message","chat_id":"group","msg_type":"merge_forward"},{"message_id":"child","upper_message_id":"message","msg_type":"text","body":{"content":"{\"text\":\"forwarded actual child\"}"}}]}}`)
	})
	event := durableIngressEvent()
	event.Event.Message.MessageType = stringRef("merge_forward")
	event.Event.Message.Content = stringRef(`{}`)
	raw, _, _ := g.rawMessageEnvelope(event)
	got, err := g.ExpandTeamMessage(t.Context(), raw)
	if err != nil || !got.Complete || !strings.Contains(got.Text, "forwarded actual child") {
		t.Fatalf("forward %+v %v", got, err)
	}
	raw.AppID = "foreign"
	if _, err = g.ExpandTeamMessage(t.Context(), raw); err == nil {
		t.Fatal("foreign gateway accepted")
	}
}
