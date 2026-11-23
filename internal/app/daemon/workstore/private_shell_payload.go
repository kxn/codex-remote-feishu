package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The fixed cat payload may reference only the exact queued durable images
// materialized by the trusted prompt builder. Paths remain data, never shell.
func (t *Tx) validatePrivateShellPayloadSources(payload, scope string, inputs []work.Inbox) error {
	const start = "<queued_input_bundle_v1>\n"
	const end = "\n</queued_input_bundle_v1>\n"
	if !validPrivateShellBundle(payload) {
		return ErrScopeDenied
	}
	var bundle struct {
		Attachments []struct {
			Ref, Type, Path string
			MIMEType        string `json:"mime_type"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(payload, start), end)), &bundle); err != nil {
		return err
	}
	assets := map[string][]work.MessageAttachment{}
	seenRefs := map[string]bool{}
	assetCount := 0
	for _, input := range inputs {
		message, err := t.GetMessage(input.MessageID)
		if err != nil {
			return err
		}
		for _, asset := range message.Attachments {
			if asset.Kind == "image" && !seenRefs[asset.ContentRef] {
				seenRefs[asset.ContentRef] = true
				assetCount++
				key := asset.SHA256 + "\x00" + asset.MediaType
				assets[key] = append(assets[key], asset)
			}
		}
	}
	if len(bundle.Attachments) != assetCount {
		return ErrScopeDenied
	}
	scopeSum := sha256.Sum256([]byte(scope))
	scopeDir := hex.EncodeToString(scopeSum[:])
	for _, item := range bundle.Attachments {
		if item.Type != "image" || !filepath.IsAbs(item.Path) || filepath.Base(filepath.Dir(item.Path)) != scopeDir {
			return ErrScopeDenied
		}
		hash := strings.TrimSuffix(filepath.Base(item.Path), filepath.Ext(item.Path))
		key := hash + "\x00" + item.MIMEType
		matching := assets[key]
		if len(matching) == 0 {
			return ErrScopeDenied
		}
		asset := matching[0]
		assets[key] = matching[1:]
		info, err := os.Lstat(item.Path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != asset.Size {
			return ErrScopeDenied
		}
		raw, err := os.ReadFile(item.Path)
		if err != nil {
			return err
		}
		actual := sha256.Sum256(raw)
		if hex.EncodeToString(actual[:]) != asset.SHA256 {
			return fmt.Errorf("queued shell image bytes changed before send")
		}
	}
	return nil
}
