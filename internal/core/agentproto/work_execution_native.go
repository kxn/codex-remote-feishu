package agentproto

const EventNativeTurnBindingObserved EventKind = "native.turn.binding.observed"
const EventNativeInputReceipt EventKind = "native.input.receipt"

// EventNativeCommandRejected is a private wrapper's durable, pre-write
// rejection. It is not an acknowledgement or a native provider response.
const EventNativeCommandRejected EventKind = "native.command.rejected"
