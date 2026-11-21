package agentproto

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const JournalProtocolVersion = 1

const EnvelopeEventReceipt EnvelopeType = "event_receipt"

type RuntimeEventIdentity struct {
	Version            int    `json:"version"`
	CarrierIncarnation string `json:"carrierIncarnation"`
	Sequence           uint64 `json:"sequence"`
	PayloadHash        string `json:"payloadHash"`
}

// EventReceipt acknowledges one durably stored event, never an inferred range.
type EventReceipt struct {
	InstanceID         string `json:"instanceId,omitempty"`
	CarrierIncarnation string `json:"carrierIncarnation"`
	Sequence           uint64 `json:"sequence"`
	PayloadHash        string `json:"payloadHash"`
}

type RuntimeCommandReceipt struct {
	Version            int                   `json:"version"`
	CarrierIncarnation string                `json:"carrierIncarnation"`
	CommandID          string                `json:"commandId"`
	PayloadHash        string                `json:"payloadHash"`
	Stage              string                `json:"stage"`
	PreparedFrames     int                   `json:"preparedFrames"`
	WrittenFrames      int                   `json:"writtenFrames"`
	Frames             []RuntimeFrameReceipt `json:"frames,omitempty"`
}

type RuntimeFrameReceipt struct {
	FrameID         int64  `json:"frameId"`
	NativeRequestID string `json:"nativeRequestId,omitempty"`
	NativeMethod    string `json:"nativeMethod,omitempty"`
	Stage           string `json:"stage"`
}

func RuntimeEventPayloadHash(event Event) (string, error) {
	event.Journal = nil
	raw, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
