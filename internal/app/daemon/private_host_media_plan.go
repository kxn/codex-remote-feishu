package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type privateStagedFile struct {
	Source, Target, Name, SourceMessageID, SHA256 string
	Size                                          int64
}
type privateStageIntent struct {
	Version                      int
	Workspace, WorkspaceIdentity string
	Files                        []privateStagedFile
}
type privateStageReceipt struct {
	Cancelled           bool   `json:",omitempty"`
	LegacyTargetUnknown bool   `json:",omitempty"`
	Failed              bool   `json:",omitempty"`
	FailureReason       string `json:",omitempty"`
	InternalError       string `json:",omitempty"`
	Version             int
	EffectID, SurfaceID string
	Files               []privateStagedFile
}

func privateSourceHash(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, fmt.Errorf("attachment source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("attachment source is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", 0, fmt.Errorf("attachment source changed")
	}
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func privateBuildStageIntent(workspace, identity string, a control.Action) (privateStageIntent, error) {
	intent := privateStageIntent{Version: 1, Workspace: workspace, WorkspaceIdentity: identity}
	files := a.Files
	if a.Kind == control.ActionFileMessage {
		files = []control.ActionFileAttachment{{SourceMessageID: a.MessageID, LocalPath: a.LocalPath, FileName: a.FileName}}
	}
	for _, file := range files {
		if strings.TrimSpace(file.LocalPath) == "" {
			continue
		}
		message := strings.TrimSpace(file.SourceMessageID)
		if message == "" {
			message = a.MessageID
		}
		segment := sanitizeInboundWorkspacePathSegment(message)
		if segment == "" {
			return intent, fmt.Errorf("attachment source identity missing")
		}
		name := sanitizeInboundWorkspaceFileName(file.FileName)
		target := filepath.Join(workspaceSurfaceContextDir, inboundWorkspaceFileInboxDir, inboundWorkspaceFileSource, segment, name)
		hash, size, err := privateSourceHash(file.LocalPath)
		if err != nil {
			return intent, err
		}
		intent.Files = append(intent.Files, privateStagedFile{Source: file.LocalPath, Target: target, Name: name, SourceMessageID: message, SHA256: hash, Size: size})
	}
	if len(intent.Files) == 0 {
		return intent, fmt.Errorf("attachment source missing")
	}
	return intent, nil
}
