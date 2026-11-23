package agentproto

// NativeShellItemObserved records the actual session-level UserShell item.
// Its owner binding carries no model attempt. Thread/turn/item are native facts,
// not the locally expected turn from the original human action's authorization.
const EventNativeShellItemObserved EventKind = "native.shell.item.observed"

// RPCResult acknowledges only the native session-level request. It does not
// prove a turn, an executed item, or consumed model input.
const EventNativeShellRPCResult EventKind = "native.shell.rpc.result"
