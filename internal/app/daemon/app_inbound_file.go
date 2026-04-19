package daemon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	inboundWorkspaceFileInboxDir = "inbox"
	inboundWorkspaceFileSource   = "feishu-files"
)

func (a *App) applyIngressActionLocked(action control.Action) []control.UIEvent {
	if action.Kind != control.ActionFileMessage {
		return a.service.ApplySurfaceAction(action)
	}

	prepared, err := a.prepareInboundFileActionLocked(action)
	if err != nil {
		a.ensureSurfaceRouteForNotice(action)
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			GatewayID:        action.GatewayID,
			SurfaceSessionID: action.SurfaceSessionID,
			Notice: &control.Notice{
				Code:     "inbound_file_prepare_failed",
				Title:    "文件暂存失败",
				Text:     "文件已经收到，但暂存到当前工作区时失败了，请稍后重试。",
				ThemeKey: "error",
			},
		}}
	}

	events := a.service.ApplySurfaceAction(prepared)
	if path := strings.TrimSpace(prepared.LocalPath); path != "" && !a.inboundFileRetainedLocked(prepared.SurfaceSessionID, prepared.MessageID, path) {
		if err := removeInboundWorkspaceFile(path); err != nil {
			a.debugf("cleanup unretained inbound file failed: surface=%s message=%s path=%s err=%v", prepared.SurfaceSessionID, prepared.MessageID, path, err)
		}
	}
	return events
}

func (a *App) prepareInboundFileActionLocked(action control.Action) (control.Action, error) {
	path := strings.TrimSpace(action.LocalPath)
	if action.Kind != control.ActionFileMessage || path == "" {
		return action, nil
	}
	surface := a.service.Surface(action.SurfaceSessionID)
	if surface == nil {
		return action, nil
	}
	inst := a.service.Instance(strings.TrimSpace(surface.AttachedInstanceID))
	if inst == nil {
		return action, nil
	}
	workspaceRoot := state.NormalizeWorkspaceKey(inst.WorkspaceRoot)
	if workspaceRoot == "" {
		return action, fmt.Errorf("attached instance %s missing workspace root", strings.TrimSpace(inst.InstanceID))
	}
	finalPath, err := materializeInboundWorkspaceFile(workspaceRoot, action.MessageID, action.FileName, path)
	if err != nil {
		_ = removeInboundWorkspaceFile(path)
		return action, err
	}
	if err := ensureWorkspaceContextGitExclude(workspaceRoot); err != nil {
		_ = removeInboundWorkspaceFile(finalPath)
		return action, err
	}
	action.LocalPath = finalPath
	if strings.TrimSpace(action.FileName) == "" {
		action.FileName = filepath.Base(finalPath)
	}
	return action, nil
}

func (a *App) inboundFileRetainedLocked(surfaceID, sourceMessageID, path string) bool {
	surface := a.service.Surface(surfaceID)
	if surface == nil {
		return false
	}
	for _, file := range surface.StagedFiles {
		if file == nil {
			continue
		}
		if strings.TrimSpace(file.SourceMessageID) == strings.TrimSpace(sourceMessageID) || sameCleanPath(file.LocalPath, path) {
			switch file.State {
			case state.FileStaged, state.FileBound:
				return true
			}
		}
	}
	return false
}

func materializeInboundWorkspaceFile(workspaceRoot, messageID, fileName, srcPath string) (string, error) {
	workspaceRoot = state.NormalizeWorkspaceKey(workspaceRoot)
	srcPath = strings.TrimSpace(srcPath)
	if workspaceRoot == "" || srcPath == "" {
		return "", fmt.Errorf("invalid inbound file materialization input")
	}
	messageSegment := sanitizeInboundWorkspacePathSegment(messageID)
	if messageSegment == "" {
		messageSegment = "unknown-message"
	}
	fileSegment := sanitizeInboundWorkspaceFileName(fileName)
	targetDir := filepath.Join(workspaceRoot, workspaceSurfaceContextDir, inboundWorkspaceFileInboxDir, inboundWorkspaceFileSource, messageSegment)
	targetPath := filepath.Join(targetDir, fileSegment)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	if sameCleanPath(srcPath, targetPath) {
		return targetPath, nil
	}
	if err := moveFileWithFallback(srcPath, targetPath); err != nil {
		return "", err
	}
	return targetPath, nil
}

func sanitizeInboundWorkspacePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.NewReplacer("\n", "-", "\r", "-", "\\", "-", "/", "-", ":", "-", "\t", "-").Replace(value)
	value = strings.Trim(value, ". ")
	if value == "" {
		return ""
	}
	return value
}

func sanitizeInboundWorkspaceFileName(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.TrimSpace(name)))
	if name == "" || name == "." || name == ".." {
		return "attachment.bin"
	}
	name = strings.NewReplacer("\n", "-", "\r", "-", "\\", "-", "/", "-", "\t", "-", ":", "-").Replace(name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "attachment.bin"
	}
	return name
}

func moveFileWithFallback(srcPath, dstPath string) error {
	if err := os.Rename(srcPath, dstPath); err == nil {
		return nil
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	copyErr := func() error {
		if _, err := io.Copy(dst, src); err != nil {
			_ = dst.Close()
			return err
		}
		return dst.Close()
	}()
	if copyErr != nil {
		return copyErr
	}
	return os.Remove(srcPath)
}

func removeInboundWorkspaceFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	for i := 0; i < 4; i++ {
		if parent == "" || parent == "." || parent == string(filepath.Separator) {
			break
		}
		if err := os.Remove(parent); err != nil {
			break
		}
		parent = filepath.Dir(parent)
	}
	return nil
}

func sameCleanPath(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if abs, err := filepath.Abs(left); err == nil {
		left = abs
	}
	if abs, err := filepath.Abs(right); err == nil {
		right = abs
	}
	return left == right
}
