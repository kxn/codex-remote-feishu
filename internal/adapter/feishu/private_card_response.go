package feishu

import (
	"encoding/json"
	"fmt"
)

// FreezePrivateCardResponse renders the same callback response as the gateway.
// Rendering is local and performs no transport or filesystem operations.
func FreezePrivateCardResponse(op Operation) (json.RawMessage, error) {
	response := callbackCardResponse(&ActionResult{ReplaceCurrentCard: &op})
	if response == nil {
		return nil, fmt.Errorf("not a card operation")
	}
	return json.Marshal(response)
}
