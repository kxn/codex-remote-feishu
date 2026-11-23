package agentproto

import (
	"encoding/json"
	"fmt"
	"strings"
)

type queuedInputBundle struct {
	Schema      string                  `json:"schema"`
	Text        string                  `json:"text"`
	Attachments []queuedInputAttachment `json:"attachments"`
}

type queuedInputAttachment struct {
	Ref      string `json:"ref"`
	Type     string `json:"type"`
	Path     string `json:"path"`
	MIMEType string `json:"mime_type"`
}

// QueuedInputShellPayload encodes the original queued-input fixed-reader payload.
func QueuedInputShellPayload(inputs []Input) (string, bool, error) {
	var textParts []string
	attachments := make([]queuedInputAttachment, 0)
	for _, input := range inputs {
		switch input.Type {
		case InputText:
			if input.Text != "" {
				textParts = append(textParts, input.Text)
			}
		case InputLocalImage:
			path := strings.TrimSpace(input.Path)
			if path == "" {
				return "", false, fmt.Errorf("queued image attachment has no local path")
			}
			attachments = append(attachments, queuedInputAttachment{
				Ref:      fmt.Sprintf("att-%d", len(attachments)+1),
				Type:     "image",
				Path:     path,
				MIMEType: strings.TrimSpace(input.MIMEType),
			})
		case InputRemoteImage:
			path := strings.TrimSpace(input.Path)
			if path == "" {
				path = strings.TrimSpace(input.URL)
			}
			if path == "" {
				return "", false, fmt.Errorf("queued remote image attachment has no reference")
			}
			attachments = append(attachments, queuedInputAttachment{
				Ref:      fmt.Sprintf("att-%d", len(attachments)+1),
				Type:     "image",
				Path:     path,
				MIMEType: strings.TrimSpace(input.MIMEType),
			})
		}
	}
	text := strings.Join(textParts, "\n")
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return "", false, nil
	}
	payload, err := json.MarshalIndent(queuedInputBundle{
		Schema:      "queued_input_bundle.v1",
		Text:        text,
		Attachments: attachments,
	}, "", "  ")
	if err != nil {
		return "", false, err
	}
	return "<queued_input_bundle_v1>\n" + string(payload) + "\n</queued_input_bundle_v1>\n", true, nil
}
