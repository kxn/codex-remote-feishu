package runtimejournal

import (
	"encoding/base64"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worklocation"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

// Freeze an image at the native event boundary. The daemon publishes durable
// bytes; resuming/replaying never dereferences a path from the native process.
func (s *Store) freezeImageOutput(event agentproto.Event) agentproto.Event {
	if event.ItemKind != "image_generation" || xutil.MetadataString(event.Metadata, "imageBase64") != "" {
		return event
	}
	path := xutil.MetadataString(event.Metadata, "savedPath")
	if path == "" {
		return event
	}
	if filepath.IsAbs(path) {
		relative, err := filepath.Rel(s.workspaceRoot, path)
		if err != nil {
			return event
		}
		path = relative
	}
	data, err := worklocation.ReadDeliveryFile(s.workspaceRoot, path)
	if err != nil || len(data) > 10_000_000 || !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return event
	}
	name, _ := worklocation.DeliveryFileName(path)
	event = attachDisplayValue(event, "imageFileName", name)
	return attachDisplayValue(event, "imageBase64", base64.StdEncoding.EncodeToString(data))
}
