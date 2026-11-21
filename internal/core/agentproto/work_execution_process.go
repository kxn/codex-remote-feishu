package agentproto

type RuntimeProcessIdentity struct {
	HostID        string `json:"host_id,omitempty"`
	Platform      string `json:"platform"`
	PID           int    `json:"pid"`
	StartIdentity string `json:"start_identity"`
}
