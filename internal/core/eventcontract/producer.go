package eventcontract

func NewEventFromPayload(payload Payload, meta EventMeta) Event {
	return Event{
		Meta:    meta,
		Payload: payload,
	}.Normalized()
}
