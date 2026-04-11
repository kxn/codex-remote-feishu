package feishu

import (
	"strconv"
	"strings"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func inboundMetaFromMessageEvent(event *larkim.P2MessageReceiveV1) *control.ActionInboundMeta {
	if event == nil {
		return nil
	}
	meta := newInboundMeta(headerFromMessageEvent(event), requestIDFromEventReq(event.EventReq))
	if event.Event != nil && event.Event.Message != nil {
		meta.MessageCreateTime = parseEpochString(stringPtr(event.Event.Message.CreateTime))
		meta.OpenMessageID = strings.TrimSpace(stringPtr(event.Event.Message.MessageId))
	}
	return meta
}

func inboundMetaFromMessageRecalledEvent(event *larkim.P2MessageRecalledV1) *control.ActionInboundMeta {
	if event == nil {
		return nil
	}
	meta := newInboundMeta(headerFromMessageRecalledEvent(event), requestIDFromEventReq(event.EventReq))
	if event.Event != nil {
		meta.OpenMessageID = strings.TrimSpace(stringPtr(event.Event.MessageId))
	}
	return meta
}

func inboundMetaFromMessageReactionCreatedEvent(event *larkim.P2MessageReactionCreatedV1) *control.ActionInboundMeta {
	if event == nil {
		return nil
	}
	meta := newInboundMeta(headerFromMessageReactionCreatedEvent(event), requestIDFromEventReq(event.EventReq))
	if event.Event != nil {
		meta.OpenMessageID = strings.TrimSpace(stringPtr(event.Event.MessageId))
	}
	return meta
}

func inboundMetaFromMenuEvent(event *larkapplication.P2BotMenuV6) *control.ActionInboundMeta {
	if event == nil {
		return nil
	}
	meta := newInboundMeta(headerFromMenuEvent(event), requestIDFromEventReq(event.EventReq))
	if event.Event != nil && event.Event.Timestamp != nil {
		meta.MenuClickTime = parseEpochInt64(*event.Event.Timestamp)
	}
	return meta
}

func inboundMetaFromCardActionEvent(event *larkcallback.CardActionTriggerEvent) *control.ActionInboundMeta {
	if event == nil {
		return nil
	}
	meta := newInboundMeta(headerFromCardActionEvent(event), requestIDFromEventReq(event.EventReq))
	if event.Event != nil {
		if event.Event.Context != nil {
			meta.OpenMessageID = strings.TrimSpace(event.Event.Context.OpenMessageID)
		}
		if event.Event.Action != nil {
			meta.CardDaemonLifecycleID = strings.TrimSpace(stringMapValue(event.Event.Action.Value, cardActionPayloadKeyDaemonLifecycleID))
		}
	}
	return meta
}

func newInboundMeta(header *larkevent.EventHeader, requestID string) *control.ActionInboundMeta {
	meta := &control.ActionInboundMeta{
		RequestID: strings.TrimSpace(requestID),
	}
	if header == nil {
		return meta
	}
	meta.EventID = strings.TrimSpace(header.EventID)
	meta.EventType = strings.TrimSpace(header.EventType)
	meta.EventCreateTime = parseEpochString(header.CreateTime)
	return meta
}

func requestIDFromEventReq(req *larkevent.EventReq) string {
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.RequestId())
}

func headerFromMessageEvent(event *larkim.P2MessageReceiveV1) *larkevent.EventHeader {
	if event == nil || event.EventV2Base == nil {
		return nil
	}
	return event.EventV2Base.Header
}

func headerFromMessageRecalledEvent(event *larkim.P2MessageRecalledV1) *larkevent.EventHeader {
	if event == nil || event.EventV2Base == nil {
		return nil
	}
	return event.EventV2Base.Header
}

func headerFromMessageReactionCreatedEvent(event *larkim.P2MessageReactionCreatedV1) *larkevent.EventHeader {
	if event == nil || event.EventV2Base == nil {
		return nil
	}
	return event.EventV2Base.Header
}

func headerFromMenuEvent(event *larkapplication.P2BotMenuV6) *larkevent.EventHeader {
	if event == nil || event.EventV2Base == nil {
		return nil
	}
	return event.EventV2Base.Header
}

func headerFromCardActionEvent(event *larkcallback.CardActionTriggerEvent) *larkevent.EventHeader {
	if event == nil || event.EventV2Base == nil {
		return nil
	}
	return event.EventV2Base.Header
}

func parseEpochString(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return parseEpochInt64(value)
}

func parseEpochInt64(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	switch {
	case value >= 1e17:
		return time.Unix(0, value).UTC()
	case value >= 1e14:
		return time.UnixMicro(value).UTC()
	case value >= 1e11:
		return time.UnixMilli(value).UTC()
	default:
		return time.Unix(value, 0).UTC()
	}
}
