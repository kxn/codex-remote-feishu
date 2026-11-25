package agentproto

import (
	"encoding/json"
	"time"
)

const (
	RuntimeInspectionProtocolVersion                   = 1
	PrivateNativeInspectionProtocolVersion             = 1
	CommandRuntimeInspect                  CommandKind = "runtime.inspect"
	EventRuntimeInspectionCompleted        EventKind   = "runtime.inspection.completed"
)

type RuntimeInspectionRequest struct {
	SubjectCommandID           string `json:"subjectCommandId,omitempty"`
	Version                    int    `json:"version"`
	ExpectedInstanceGeneration int64  `json:"expectedInstanceGeneration"`
	ExpectedProcessGeneration  int64  `json:"expectedProcessGeneration"`
	ExpectedLocationGeneration int64  `json:"expectedLocationGeneration"`
	StopBackground             bool   `json:"stopBackground,omitempty"`
	IncludePendingRequests     bool   `json:"includePendingRequests,omitempty"`
}

// RuntimePendingRequestInventory is a versioned observation of an exact
// carrier's durable request stream. Incomplete inventories cannot restore UI.
type RuntimePendingRequestInventory struct {
	Version            int                     `json:"version"`
	Complete           bool                    `json:"complete"`
	CarrierIncarnation string                  `json:"carrierIncarnation"`
	InstanceGeneration int64                   `json:"instanceGeneration"`
	ProcessGeneration  int64                   `json:"processGeneration"`
	EventSequence      uint64                  `json:"eventSequence"`
	Requests           []RuntimePendingRequest `json:"requests"`
}

type RuntimePendingRequest struct {
	RequestID       string `json:"requestId"`
	ThreadID        string `json:"threadId"`
	TurnID          string `json:"turnId"`
	StartedSequence uint64 `json:"startedSequence"`
	StartedHash     string `json:"startedHash"`
}

type RPCProbe struct {
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

type BackgroundInspection struct {
	ActiveNativeTurnIDs           []string        `json:"active_native_turn_ids,omitempty"`
	ContinuationInventoryKnown    bool            `json:"continuation_inventory_known"`
	ActiveGoalSessionIDs          []string        `json:"active_goal_session_ids,omitempty"`
	CarrierIncarnation            string          `json:"carrier_incarnation"`
	ProcessGeneration             int64           `json:"process_generation"`
	LocationGeneration            int64           `json:"location_generation"`
	Complete                      bool            `json:"complete"`
	NativeTaskSetComplete         bool            `json:"native_task_set_complete"`
	NativeInputsComplete          bool            `json:"native_inputs_complete"`
	RunningProcessIDs             []string        `json:"running_process_ids"`
	PendingTaskIDs                []string        `json:"pending_task_ids"`
	PendingNativeInputs           int             `json:"pending_native_inputs"`
	ExternalResourceUncertainties []string        `json:"external_resource_uncertainties,omitempty"`
	Evidence                      json.RawMessage `json:"evidence"`
}

type RuntimeInspectionResult struct {
	PendingRequests      *RuntimePendingRequestInventory `json:"pendingRequests,omitempty"`
	NativeWriteWatermark int64                           `json:"nativeWriteWatermark"`
	InstanceGeneration   int64                           `json:"instanceGeneration"`
	ProcessGeneration    int64                           `json:"processGeneration"`
	LocationGeneration   int64                           `json:"locationGeneration"`
	Bootstrap            RPCProbe                        `json:"bootstrap"`
	OwnerScope           *RuntimeOwnerInspection         `json:"ownerScope,omitempty"`
	Version              int                             `json:"version"`
	RequestCommandID     string                          `json:"requestCommandId"`
	CarrierIncarnation   string                          `json:"carrierIncarnation"`
	Native               RPCProbe                        `json:"native"`
	Continuation         RPCProbe                        `json:"continuation"`
	StreamTerminal       *Event                          `json:"streamTerminal,omitempty"`
	NativeRejection      *Event                          `json:"nativeRejection,omitempty"`
	Background           BackgroundInspection            `json:"background"`
	ObservedAt           time.Time                       `json:"observedAt"`
	Problem              *ErrorInfo                      `json:"problem,omitempty"`
}

type RuntimeOwnerInspection struct {
	Bootstrap             RPCProbe                   `json:"bootstrap"`
	KnownSessionsComplete bool                       `json:"knownSessionsComplete"`
	Sessions              []RuntimeSessionInspection `json:"sessions"`
}

type RuntimeSessionInspection struct {
	SessionID             string   `json:"sessionId"`
	Native                RPCProbe `json:"native"`
	Continuation          RPCProbe `json:"continuation"`
	StreamTerminal        *Event   `json:"streamTerminal,omitempty"`
	NoNativeInputObserved bool     `json:"noNativeInputObserved,omitempty"`
}
