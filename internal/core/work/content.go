package work

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Content is an immutable durable body. References contain ID, never the body
// itself. Scope ownership remains mandatory even when hashes happen to match.
type Content struct {
	ID        string    `json:"id"`
	ScopeID   string    `json:"scope_id"`
	MediaType string    `json:"media_type"`
	Data      []byte    `json:"data"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

func NewContent(id, scopeID, mediaType string, data []byte, at time.Time) Content {
	sum := sha256.Sum256(data)
	return Content{ID: id, ScopeID: scopeID, MediaType: mediaType, Data: append([]byte(nil), data...), SHA256: hex.EncodeToString(sum[:]), CreatedAt: at}
}
func (v Content) Validate() error {
	if err := required(v.ID, v.ScopeID, v.MediaType, v.SHA256); err != nil {
		return err
	}
	sum := sha256.Sum256(v.Data)
	if v.SHA256 != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("content digest mismatch")
	}
	if v.CreatedAt.IsZero() {
		return fmt.Errorf("content creation time required")
	}
	return nil
}
