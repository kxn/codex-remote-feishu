package agentproto

const NativeMaintenanceProtocolVersion = 1
const EventNativeMaintenanceStarted EventKind = "native.maintenance.started"
const EventNativeMaintenanceTerminal EventKind = "native.maintenance.terminal"

type NativeMaintenanceCommand struct {
	Version                      int    `json:"version"`
	PreflightInspectionCommandID string `json:"preflightInspectionCommandId"`
	NativeWriteWatermark         int64  `json:"nativeWriteWatermark"`
}
