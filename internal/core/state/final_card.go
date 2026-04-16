package state

import "time"

type FinalCardRecord struct {
	InstanceID        string
	ThreadID          string
	TurnID            string
	ItemID            string
	SourceMessageID   string
	MessageID         string
	DaemonLifecycleID string
	RecordedAt        time.Time
}
