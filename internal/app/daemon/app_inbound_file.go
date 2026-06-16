package daemon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	inboundWorkspaceFileInboxDir = "inbox"
	inboundWorkspaceFileSource   = "feishu-files"
)

func (a *App) applyIngressActionLocked(action control.Action) []eventcontract.Event {
	switch action.Kind {
	case control.ActionFileMessage:
		return a.applyIngressFileActionLocked(action)
	case control.ActionTextMessage:
		prepared, err := a.prepareInboundTextFilesActionLocked(action)
		if err != nil {
			a.ensureSurfaceRouteForNotice(action)
			return []eventcontract.Event{{
				Kind:             eventcontract.KindNotice,
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
		return a.service.ApplySurfaceAction(prepared)
	default:
		return a.service.ApplySurfaceAction(action)
	}
}

func (a *App) applyIngressFileActionLocked(action control.Action) []eventcontract.Event {
	prepared, err := a.prepareInboundFileActionLocked(action)
	if err != nil {
		a.ensureSurfaceRouteForNotice(action)
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
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

func (a *App) prepareInboundTextFilesActionLocked(action control.Action) (control.Action, error) {
	if action.Kind != control.ActionTextMessage || len(action.Files) == 0 {
		return action, nil
	}
	preparedFiles := make([]control.ActionFileAttachment, 0, len(action.Files))
	for _, file := range action.Files {
		path := strings.TrimSpace(file.LocalPath)
		if path == "" {
			continue
		}
		sourceMessageID := strings.TrimSpace(file.SourceMessageID)
		if sourceMessageID == "" {
			sourceMessageID = action.MessageID
		}
		prepared, err := a.prepareInboundFileActionLocked(control.Action{
			Kind:             control.ActionFileMessage,
			GatewayID:        action.GatewayID,
			SurfaceSessionID: action.SurfaceSessionID,
			ChatID:           action.ChatID,
			ActorUserID:      action.ActorUserID,
			MessageID:        sourceMessageID,
			LocalPath:        path,
			FileName:         file.FileName,
			Inbound:          action.Inbound,
		})
		if err != nil {
			return action, err
		}
		if strings.TrimSpace(prepared.LocalPath) == "" {
			continue
		}
		preparedFiles = append(preparedFiles, control.ActionFileAttachment{
			SourceMessageID: sourceMessageID,
			LocalPath:       prepared.LocalPath,
			FileName:        prepared.FileName,
		})
	}
	if prompt := inboundFileAttachmentPrompt(preparedFiles); prompt != "" {
		fileInput := agentproto.Input{Type: agentproto.InputText, Text: prompt}
		action.Inputs = append([]agentproto.Input{fileInput}, action.Inputs...)
	}
	return action, nil
}

func inboundFileAttachmentPrompt(files []control.ActionFileAttachment) string {
	if len(files) == 0 {
		return ""
	}
	lines := []string{"附带参考文件（内容未直接注入上下文，可按需读取以下本地路径）："}
	for _, file := range files {
		path := strings.TrimSpace(file.LocalPath)
		if path == "" {
			continue
		}
		name := strings.TrimSpace(file.FileName)
		if name == "" {
			name = filepath.Base(path)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", name, path))
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
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
