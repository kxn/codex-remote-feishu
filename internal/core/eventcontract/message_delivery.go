package eventcontract

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
}

func (delivery MessageDelivery) Normalized() MessageDelivery {
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
