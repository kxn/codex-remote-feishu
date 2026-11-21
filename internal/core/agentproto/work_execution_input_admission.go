package agentproto

const NativeInputAdmissionProtocolVersion = 1
const EventNativeTurnInputAccepted EventKind = "native.turn.input.accepted"
const EventNativeTurnInputRejected EventKind = "native.turn.input.rejected"

type NativeInputAdmissionCommand struct {
	Version  int      `json:"version"`
	ID       string   `json:"id"`
	InputIDs []string `json:"inputIds"`
}
