package claude

// BootstrapFrames returns the initial frame to send to the Claude CLI on startup.
// This sends the SDK initialize control_request. The CLI will respond with MCP
// setup requests (handled in ObserveServer) before completing initialization.
func (t *Translator) BootstrapFrames(source string, version string) ([][]byte, error) {
	t.pendingInitID = t.nextRequest("init")

	payload := map[string]any{
		"type":       "control_request",
		"request_id": t.pendingInitID,
		"request": map[string]any{
			"subtype": "initialize",
		},
	}

	bytes, err := marshalNDJSON(payload)
	if err != nil {
		return nil, err
	}

	t.debugf("bootstrap: sending initialize request=%s", t.pendingInitID)
	return [][]byte{bytes}, nil
}
