package feishu

import (
	"context"
	"errors"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"testing"
)

func TestSendIMMediaKeepsTopicAndCrossChatTargetsSeparate(t *testing.T) {
	for _, kind := range []string{"image", "file", "media"} {
		for _, target := range []string{"oc_chat", "oc_other"} {
			for _, fail := range []bool{false, true} {
				t.Run(kind+"/"+target+map[bool]string{true: "/failed", false: "/success"}[fail], func(t *testing.T) {
					gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
					surface := "feishu:app-1:chat:oc_chat@om_a"
					gateway.recordSurfaceMessage("om_a", surface)
					gateway.uploadImagePathFn = func(context.Context, string) (string, error) { return "img", nil }
					gateway.uploadFilePathFn = func(context.Context, string) (string, string, error) { return "file", "test.txt", nil }
					gateway.uploadVideoPathFn = func(context.Context, string) (string, string, error) { return "media", "test.mp4", nil }
					failure := errors.New("send failed")
					gateway.replyMessageFn = func(_ context.Context, id, ty, _ string, inThread bool) (*larkim.ReplyMessageResp, error) {
						if target != "oc_chat" || id != "om_a" || ty != kind || !inThread {
							t.Fatalf("unexpected reply: %s/%s/%v", id, ty, inThread)
						}
						if fail {
							return nil, failure
						}
						return &larkim.ReplyMessageResp{Data: &larkim.ReplyMessageRespData{MessageId: stringRef("om_media")}}, nil
					}
					gateway.createMessageFn = func(_ context.Context, idType, id, ty, _ string) (*larkim.CreateMessageResp, error) {
						if target != "oc_other" || idType != "chat_id" || id != target || ty != kind {
							t.Fatalf("unexpected create: %s/%s/%s", idType, id, ty)
						}
						if fail {
							return nil, failure
						}
						return &larkim.CreateMessageResp{Data: &larkim.CreateMessageRespData{MessageId: stringRef("om_media")}}, nil
					}
					var id string
					var err error
					switch kind {
					case "image":
						r, e := gateway.SendIMImage(t.Context(), IMImageSendRequest{GatewayID: "app-1", SurfaceSessionID: surface, ChatID: target, Path: "test.png"})
						id, err = r.MessageID, e
					case "file":
						r, e := gateway.SendIMFile(t.Context(), IMFileSendRequest{GatewayID: "app-1", SurfaceSessionID: surface, ChatID: target, Path: "test.txt"})
						id, err = r.MessageID, e
					case "media":
						r, e := gateway.SendIMVideo(t.Context(), IMVideoSendRequest{GatewayID: "app-1", SurfaceSessionID: surface, ChatID: target, Path: "test.mp4"})
						id, err = r.MessageID, e
					}
					if fail {
						if !errors.Is(err, failure) {
							t.Fatalf("error=%v, want original", err)
						}
					} else if err != nil || id != "om_media" {
						t.Fatalf("result=%q/%v", id, err)
					}
					wantAnchor := "om_a"
					if !fail && target == "oc_chat" {
						wantAnchor = "om_media"
					}
					if got := gateway.surfaceReplyAnchor(surface); got != wantAnchor {
						t.Fatalf("anchor=%q want=%q", got, wantAnchor)
					}
					if target == "oc_other" && gateway.lookupSurfaceMessage("om_media") != "" {
						t.Fatal("cross-chat output recorded as source topic")
					}
					gateway.replyMessageFn = func(_ context.Context, id, ty, _ string, inThread bool) (*larkim.ReplyMessageResp, error) {
						if id != wantAnchor || ty != "text" || !inThread {
							t.Fatalf("followup target=%q, want=%q", id, wantAnchor)
						}
						return &larkim.ReplyMessageResp{}, nil
					}
					if err := gateway.Apply(t.Context(), []Operation{{Kind: OperationSendText, SurfaceSessionID: surface, ChatID: "oc_chat", Text: "followup"}}); err != nil {
						t.Fatal(err)
					}

				})
			}
		}
	}
}
