package agentproto

import (
	"encoding/json"
	"time"
)

const WireProtocol = "relay.agent.v1"

type EnvelopeType string

const (
	EnvelopeHello      EnvelopeType = "hello"
	EnvelopeWelcome    EnvelopeType = "welcome"
	EnvelopeEventBatch EnvelopeType = "event_batch"
	EnvelopeCommand    EnvelopeType = "command"
	EnvelopeCommandAck EnvelopeType = "command_ack"
	EnvelopePing       EnvelopeType = "ping"
	EnvelopePong       EnvelopeType = "pong"
	EnvelopeError      EnvelopeType = "error"
)

type Capabilities struct {
	ThreadsRefresh bool `json:"threadsRefresh,omitempty"`
}

type BinaryIdentity struct {
	Product          string `json:"product,omitempty"`
	Version          string `json:"version,omitempty"`
	Branch           string `json:"branch,omitempty"`
	BuildFingerprint string `json:"buildFingerprint,omitempty"`
	BinaryPath       string `json:"binaryPath,omitempty"`
}

type InstanceHello struct {
	InstanceID       string `json:"instanceId"`
	DisplayName      string `json:"displayName,omitempty"`
	WorkspaceRoot    string `json:"workspaceRoot,omitempty"`
	WorkspaceKey     string `json:"workspaceKey,omitempty"`
	ShortName        string `json:"shortName,omitempty"`
	Source           string `json:"source,omitempty"`
	Managed          bool   `json:"managed,omitempty"`
	Version          string `json:"version,omitempty"`
	Branch           string `json:"branch,omitempty"`
	BuildFingerprint string `json:"buildFingerprint,omitempty"`
	BinaryPath       string `json:"binaryPath,omitempty"`
	PID              int    `json:"pid,omitempty"`
}

type Hello struct {
	Protocol     string        `json:"protocol"`
	Probe        bool          `json:"probe,omitempty"`
	Instance     InstanceHello `json:"instance"`
	Capabilities Capabilities  `json:"capabilities,omitempty"`
}

type Welcome struct {
	Protocol   string          `json:"protocol"`
	ServerTime time.Time       `json:"serverTime,omitempty"`
	Server     *ServerIdentity `json:"server,omitempty"`
}

type ServerIdentity struct {
	BinaryIdentity
	PID        int       `json:"pid,omitempty"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
	ConfigPath string    `json:"configPath,omitempty"`
}

type EventBatch struct {
	InstanceID string  `json:"instanceId"`
	Events     []Event `json:"events"`
}

type CommandAck struct {
	InstanceID string     `json:"instanceId,omitempty"`
	CommandID  string     `json:"commandId,omitempty"`
	Accepted   bool       `json:"accepted"`
	Error      string     `json:"error,omitempty"`
	Problem    *ErrorInfo `json:"problem,omitempty"`
}

type ErrorEnvelope struct {
	Code    string     `json:"code,omitempty"`
	Message string     `json:"message"`
	Problem *ErrorInfo `json:"problem,omitempty"`
}

type Envelope struct {
	Type       EnvelopeType   `json:"type"`
	Hello      *Hello         `json:"hello,omitempty"`
	Welcome    *Welcome       `json:"welcome,omitempty"`
	EventBatch *EventBatch    `json:"eventBatch,omitempty"`
	Command    *Command       `json:"command,omitempty"`
	CommandAck *CommandAck    `json:"commandAck,omitempty"`
	Error      *ErrorEnvelope `json:"error,omitempty"`
}

func MarshalEnvelope(envelope Envelope) ([]byte, error) {
	return json.Marshal(envelope)
}

func UnmarshalEnvelope(raw []byte) (Envelope, error) {
	var envelope Envelope
	err := json.Unmarshal(raw, &envelope)
	return envelope, err
}
