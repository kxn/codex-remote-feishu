package agentproto

// PrivateEventDelivery is the single delivery contract for private native
// events. Live events are display-only and use their own sequence. All other
// facts must be journaled, acknowledged and replayable.
type PrivateEventDelivery uint8

const (
	PrivateEventDurable PrivateEventDelivery = iota
	PrivateEventLive
)

func PrivateDeliveryForEvent(kind EventKind) PrivateEventDelivery {
	if kind == EventItemDelta {
		return PrivateEventLive
	}
	return PrivateEventDurable
}

// PrivateDeliveryRequirement returns the minimum wrapper delivery version
// needed for command outcomes whose durable facts were added after private v1.
// Zero means the original wrapper has the necessary evidence.
func PrivateDeliveryRequirement(kind CommandKind) int {
	switch kind {
	case CommandRequestRespond, CommandReviewStart, CommandThreadCompactStart, CommandThreadRead:
		return PrivateDeliveryVersion
	default:
		return 0
	}
}
