package work

import "fmt"

// MessageAttachment references immutable bytes in the same scope as Message.
// Names and source content are untrusted user data, never executable authority.
type MessageAttachment struct {
	ContentRef      string `json:"content_ref"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	MediaType       string `json:"media_type"`
	FileName        string `json:"file_name,omitempty"`
	SourceMessageID string `json:"source_message_id"`
	Kind            string `json:"kind"`
}

func (v MessageAttachment) Validate() error {
	if err := required(v.ContentRef, v.SHA256, v.MediaType, v.SourceMessageID); err != nil {
		return err
	}
	if v.Size < 0 || (v.Kind != "image" && v.Kind != "file") {
		return fmt.Errorf("invalid message attachment")
	}
	return nil
}
