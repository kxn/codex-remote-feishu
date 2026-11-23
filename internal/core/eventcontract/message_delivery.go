package eventcontract

import "strings"

type MessageLane string

const (
	MessageLaneDefault     MessageLane = ""
	MessageLaneTopLevel    MessageLane = "top_level"
	MessageLaneReplyThread MessageLane = "reply_thread"
)

type MessageMutation string

const (
	MessageMutationDefault           MessageMutation = ""
	MessageMutationAppendOnly        MessageMutation = "append_only"
	MessageMutationPatchSameMessage  MessageMutation = "patch_same_message"
	MessageMutationPatchTailIfLatest MessageMutation = "patch_same_message_tail_only"
)

type MessageDelivery struct {
	FirstSendLane MessageLane
	Mutation      MessageMutation
	// TargetMessageID is the confirmed message to update. It is independent
	// of SourceMessageID, which continues to identify the user's input.
	TargetMessageID string `json:",omitempty"`
}

func (delivery MessageDelivery) Normalized() MessageDelivery {
	delivery.TargetMessageID = strings.TrimSpace(delivery.TargetMessageID)
	switch delivery.FirstSendLane {
	case MessageLaneTopLevel, MessageLaneReplyThread:
	default:
		delivery.FirstSendLane = MessageLaneDefault
	}
	switch delivery.Mutation {
	case MessageMutationAppendOnly, MessageMutationPatchSameMessage, MessageMutationPatchTailIfLatest:
	default:
		delivery.Mutation = MessageMutationDefault
	}
	if delivery.Mutation != MessageMutationPatchSameMessage {
		delivery.TargetMessageID = ""
	}
	return delivery
}

func ReplyThreadAppendOnlyDelivery() MessageDelivery {
	return MessageDelivery{
		FirstSendLane: MessageLaneReplyThread,
		Mutation:      MessageMutationAppendOnly,
	}
}

func ReplyThreadPatchTailDelivery() MessageDelivery {
	return MessageDelivery{
		FirstSendLane: MessageLaneReplyThread,
		Mutation:      MessageMutationPatchTailIfLatest,
	}
}
