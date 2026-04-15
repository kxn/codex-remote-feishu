package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const defaultPathPickerTTL = 10 * time.Minute

func (s *Service) OpenPathPicker(action control.Action, req control.PathPickerRequest) []control.UIEvent {
	surface := s.ensureSurface(action)
	return s.openPathPicker(surface, action.ActorUserID, req)
}

func (s *Service) RegisterPathPickerConsumer(kind string, consumer PathPickerConsumer) {
	if s == nil {
		return
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return
	}
	if consumer == nil {
		delete(s.pathPickerConsumers, kind)
		return
	}
	s.pathPickerConsumers[kind] = consumer
}

func (s *Service) openPathPicker(surface *state.SurfaceConsoleRecord, ownerUserID string, req control.PathPickerRequest) []control.UIEvent {
	if surface == nil {
		return nil
	}
	record, err := s.newPathPickerRecord(surface, ownerUserID, req)
	if err != nil {
		return notice(surface, "path_picker_invalid", err.Error())
	}
	surface.ActivePathPicker = record
	view, err := s.buildPathPickerView(record)
	if err != nil {
		surface.ActivePathPicker = nil
		return notice(surface, "path_picker_invalid", err.Error())
	}
	return []control.UIEvent{s.pathPickerViewEvent(surface, view, false)}
}

func (s *Service) newPathPickerRecord(surface *state.SurfaceConsoleRecord, ownerUserID string, req control.PathPickerRequest) (*state.ActivePathPickerRecord, error) {
	mode, ok := statePathPickerMode(req.Mode)
	if !ok {
		return nil, fmt.Errorf("路径选择器模式无效。")
	}
	rootPath, err := resolvePathPickerRoot(req.RootPath)
	if err != nil {
		return nil, fmt.Errorf("路径选择器根目录无效：%v", err)
	}
	currentPath, selectedPath, err := resolvePathPickerInitialState(rootPath, mode, req.InitialPath)
	if err != nil {
		return nil, fmt.Errorf("路径选择器初始路径无效：%v", err)
	}
	expiresAt := s.now().Add(defaultPathPickerTTL)
	if req.ExpireAfter > 0 {
		expiresAt = s.now().Add(req.ExpireAfter)
	}
	return &state.ActivePathPickerRecord{
		PickerID:     s.nextPathPickerToken(),
		OwnerUserID:  strings.TrimSpace(firstNonEmpty(ownerUserID, surface.ActorUserID)),
		Mode:         mode,
		Title:        strings.TrimSpace(firstNonEmpty(req.Title, defaultPathPickerTitle(mode))),
		RootPath:     rootPath,
		CurrentPath:  currentPath,
		SelectedPath: selectedPath,
		ConfirmLabel: strings.TrimSpace(firstNonEmpty(req.ConfirmLabel, "确认")),
		CancelLabel:  strings.TrimSpace(firstNonEmpty(req.CancelLabel, "取消")),
		CreatedAt:    s.now(),
		ExpiresAt:    expiresAt,
		ConsumerKind: strings.TrimSpace(req.ConsumerKind),
		ConsumerMeta: cloneStringMap(req.ConsumerMeta),
	}, nil
}

func (s *Service) nextPathPickerToken() string {
	s.nextPathPickerID++
	return fmt.Sprintf("picker-%d", s.nextPathPickerID)
}

func (s *Service) handlePathPickerEnter(surface *state.SurfaceConsoleRecord, pickerID, entryName, actorUserID string) []control.UIEvent {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	resolved, err := resolvePathPickerEntry(record.RootPath, record.CurrentPath, entryName)
	if err != nil {
		return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("目标条目无效：%v", err))
	}
	if resolved.kind != state.PathPickerModeDirectory {
		return notice(surface, "path_picker_not_directory", "只能进入目录。")
	}
	record.CurrentPath = resolved.path
	record.SelectedPath = defaultSelectedPathForMode(record.Mode, record.CurrentPath, "")
	view, err := s.buildPathPickerView(record)
	if err != nil {
		return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("目录刷新失败：%v", err))
	}
	return []control.UIEvent{s.pathPickerViewEvent(surface, view, true)}
}

func (s *Service) handlePathPickerUp(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) []control.UIEvent {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	if samePath(record.CurrentPath, record.RootPath) {
		view, err := s.buildPathPickerView(record)
		if err != nil {
			return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("目录刷新失败：%v", err))
		}
		return []control.UIEvent{s.pathPickerViewEvent(surface, view, true)}
	}
	parent := filepath.Dir(record.CurrentPath)
	resolved, err := resolvePathPickerExistingTarget(record.RootPath, parent)
	if err != nil {
		return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("无法返回上一级：%v", err))
	}
	if resolved.kind != state.PathPickerModeDirectory {
		return notice(surface, "path_picker_invalid_entry", "上一级目录无效。")
	}
	record.CurrentPath = resolved.path
	record.SelectedPath = defaultSelectedPathForMode(record.Mode, record.CurrentPath, "")
	view, err := s.buildPathPickerView(record)
	if err != nil {
		return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("目录刷新失败：%v", err))
	}
	return []control.UIEvent{s.pathPickerViewEvent(surface, view, true)}
}

func (s *Service) handlePathPickerSelect(surface *state.SurfaceConsoleRecord, pickerID, entryName, actorUserID string) []control.UIEvent {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	resolved, err := resolvePathPickerEntry(record.RootPath, record.CurrentPath, entryName)
	if err != nil {
		return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("目标条目无效：%v", err))
	}
	switch record.Mode {
	case state.PathPickerModeFile:
		if resolved.kind != state.PathPickerModeFile {
			return notice(surface, "path_picker_not_file", "当前只可选择文件。")
		}
	case state.PathPickerModeDirectory:
		if resolved.kind != state.PathPickerModeDirectory {
			return notice(surface, "path_picker_not_directory", "当前只可选择目录。")
		}
	}
	record.SelectedPath = resolved.path
	view, err := s.buildPathPickerView(record)
	if err != nil {
		return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("目录刷新失败：%v", err))
	}
	return []control.UIEvent{s.pathPickerViewEvent(surface, view, true)}
}

func (s *Service) handlePathPickerConfirm(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) []control.UIEvent {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	selectedPath, err := confirmedPathPickerSelection(record)
	if err != nil {
		return notice(surface, "path_picker_selection_missing", err.Error())
	}
	result := pathPickerResultFromRecord(record, selectedPath)
	clearSurfacePathPicker(surface)
	return s.dispatchPathPickerConfirmed(surface, result)
}

func (s *Service) handlePathPickerCancel(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) []control.UIEvent {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	result := pathPickerResultFromRecord(record, currentSelectedPath(record))
	clearSurfacePathPicker(surface)
	return s.dispatchPathPickerCancelled(surface, result)
}

func (s *Service) requireActivePathPicker(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) (*state.ActivePathPickerRecord, []control.UIEvent) {
	if surface == nil || surface.ActivePathPicker == nil {
		return nil, notice(surface, "path_picker_expired", "这个路径选择器已失效，请重新发起。")
	}
	record := surface.ActivePathPicker
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(s.now()) {
		clearSurfacePathPicker(surface)
		return nil, notice(surface, "path_picker_expired", "这个路径选择器已过期，请重新发起。")
	}
	if strings.TrimSpace(pickerID) == "" || strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		return nil, notice(surface, "path_picker_expired", "这个旧路径选择器已失效，请重新发起。")
	}
	actorUserID = strings.TrimSpace(firstNonEmpty(actorUserID, surface.ActorUserID))
	if ownerUserID := strings.TrimSpace(record.OwnerUserID); ownerUserID != "" && actorUserID != "" && ownerUserID != actorUserID {
		return nil, notice(surface, "path_picker_unauthorized", "这个路径选择器只允许发起者本人操作，请让发起者继续完成或取消。")
	}
	return record, nil
}

func (s *Service) buildPathPickerView(record *state.ActivePathPickerRecord) (control.FeishuPathPickerView, error) {
	if record == nil {
		return control.FeishuPathPickerView{}, fmt.Errorf("路径选择器不存在")
	}
	current, err := resolvePathPickerExistingTarget(record.RootPath, record.CurrentPath)
	if err != nil {
		return control.FeishuPathPickerView{}, err
	}
	if current.kind != state.PathPickerModeDirectory {
		return control.FeishuPathPickerView{}, fmt.Errorf("当前路径不是目录")
	}
	view := control.FeishuPathPickerView{
		PickerID:     record.PickerID,
		Mode:         control.PathPickerMode(record.Mode),
		Title:        strings.TrimSpace(record.Title),
		RootPath:     record.RootPath,
		CurrentPath:  current.path,
		SelectedPath: currentSelectedPath(record),
		ConfirmLabel: strings.TrimSpace(firstNonEmpty(record.ConfirmLabel, "确认")),
		CancelLabel:  strings.TrimSpace(firstNonEmpty(record.CancelLabel, "取消")),
		CanGoUp:      !samePath(current.path, record.RootPath),
		CanConfirm:   canConfirmPathPicker(record),
	}
	entries, err := buildPathPickerEntries(record)
	if err != nil {
		return control.FeishuPathPickerView{}, err
	}
	view.Entries = entries
	if len(entries) == 0 {
		view.Hint = "当前目录为空。"
	}
	if record.Mode == state.PathPickerModeFile && view.SelectedPath == "" {
		view.Hint = strings.TrimSpace(firstNonEmpty(view.Hint, "请选择一个文件后再确认。"))
	}
	return view, nil
}

func buildPathPickerEntries(record *state.ActivePathPickerRecord) ([]control.FeishuPathPickerEntry, error) {
	dirEntries, err := os.ReadDir(record.CurrentPath)
	if err != nil {
		return nil, err
	}
	items := make([]control.FeishuPathPickerEntry, 0, len(dirEntries))
	for _, entry := range dirEntries {
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		item := control.FeishuPathPickerEntry{Name: name, Label: name}
		resolved, err := resolvePathPickerEntry(record.RootPath, record.CurrentPath, name)
		if err != nil {
			item.Disabled = true
			item.DisabledReason = err.Error()
			items = append(items, item)
			continue
		}
		switch resolved.kind {
		case state.PathPickerModeDirectory:
			item.Kind = control.PathPickerEntryDirectory
			item.ActionKind = control.PathPickerEntryActionEnter
			item.Selected = samePath(currentSelectedPath(record), resolved.path)
		case state.PathPickerModeFile:
			item.Kind = control.PathPickerEntryFile
			if record.Mode == state.PathPickerModeFile {
				item.ActionKind = control.PathPickerEntryActionSelect
				item.Selected = samePath(strings.TrimSpace(record.SelectedPath), resolved.path)
			} else {
				item.Disabled = true
				item.DisabledReason = "当前只可选择目录"
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind == control.PathPickerEntryDirectory
		}
		if items[i].Kind == control.PathPickerEntryDirectory {
			leftBucket := pathPickerDirectorySortBucket(items[i])
			rightBucket := pathPickerDirectorySortBucket(items[j])
			if leftBucket != rightBucket {
				return leftBucket < rightBucket
			}
		}
		leftLabel := strings.ToLower(strings.TrimSpace(firstNonEmpty(items[i].Label, items[i].Name)))
		rightLabel := strings.ToLower(strings.TrimSpace(firstNonEmpty(items[j].Label, items[j].Name)))
		if leftLabel != rightLabel {
			return leftLabel < rightLabel
		}
		return strings.TrimSpace(items[i].Name) < strings.TrimSpace(items[j].Name)
	})
	return items, nil
}

func pathPickerDirectorySortBucket(entry control.FeishuPathPickerEntry) int {
	if entry.Kind != control.PathPickerEntryDirectory {
		return 0
	}
	name := strings.TrimSpace(firstNonEmpty(entry.Label, entry.Name))
	if strings.HasPrefix(name, ".") {
		return 1
	}
	return 0
}

func clearSurfacePathPicker(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.ActivePathPicker = nil
}

func (s *Service) pruneExpiredPathPicker(surface *state.SurfaceConsoleRecord) {
	if s == nil || surface == nil || surface.ActivePathPicker == nil {
		return
	}
	expiresAt := surface.ActivePathPicker.ExpiresAt
	if expiresAt.IsZero() || expiresAt.After(s.now()) {
		return
	}
	clearSurfacePathPicker(surface)
}

func confirmedPathPickerSelection(record *state.ActivePathPickerRecord) (string, error) {
	selectedPath := currentSelectedPath(record)
	if strings.TrimSpace(selectedPath) == "" {
		switch record.Mode {
		case state.PathPickerModeDirectory:
			return "", fmt.Errorf("当前没有可确认的目录。")
		default:
			return "", fmt.Errorf("请先选择一个文件。")
		}
	}
	resolved, err := resolvePathPickerExistingTarget(record.RootPath, selectedPath)
	if err != nil {
		return "", fmt.Errorf("选中的路径已失效：%v", err)
	}
	switch record.Mode {
	case state.PathPickerModeDirectory:
		if resolved.kind != state.PathPickerModeDirectory {
			return "", fmt.Errorf("当前只可确认目录。")
		}
	case state.PathPickerModeFile:
		if resolved.kind != state.PathPickerModeFile {
			return "", fmt.Errorf("当前只可确认文件。")
		}
	}
	return resolved.path, nil
}

func currentSelectedPath(record *state.ActivePathPickerRecord) string {
	if record == nil {
		return ""
	}
	if record.Mode == state.PathPickerModeDirectory {
		if strings.TrimSpace(record.SelectedPath) != "" {
			return strings.TrimSpace(record.SelectedPath)
		}
		return strings.TrimSpace(record.CurrentPath)
	}
	return strings.TrimSpace(record.SelectedPath)
}

func pathPickerResultFromRecord(record *state.ActivePathPickerRecord, selectedPath string) control.PathPickerResult {
	if record == nil {
		return control.PathPickerResult{}
	}
	return control.PathPickerResult{
		PickerID:     strings.TrimSpace(record.PickerID),
		Mode:         control.PathPickerMode(record.Mode),
		RootPath:     strings.TrimSpace(record.RootPath),
		CurrentPath:  strings.TrimSpace(record.CurrentPath),
		SelectedPath: strings.TrimSpace(selectedPath),
		OwnerUserID:  strings.TrimSpace(record.OwnerUserID),
		ConsumerKind: strings.TrimSpace(record.ConsumerKind),
		ConsumerMeta: cloneStringMap(record.ConsumerMeta),
		CreatedAt:    record.CreatedAt,
		ExpiresAt:    record.ExpiresAt,
	}
}

func (s *Service) dispatchPathPickerConfirmed(surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
	consumer, ok := s.lookupPathPickerConsumer(result.ConsumerKind)
	if ok {
		if events := consumer.PathPickerConfirmed(s, surface, result); len(events) != 0 {
			return events
		}
	}
	if strings.TrimSpace(result.ConsumerKind) != "" && !ok {
		return notice(surface, "path_picker_consumer_missing", "当前路径选择结果缺少可用的业务处理器，请重新发起或联系维护者检查配置。")
	}
	return notice(surface, "path_picker_confirmed", fmt.Sprintf("已确认路径：`%s`。", result.SelectedPath))
}

func (s *Service) dispatchPathPickerCancelled(surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
	consumer, ok := s.lookupPathPickerConsumer(result.ConsumerKind)
	if ok {
		if events := consumer.PathPickerCancelled(s, surface, result); len(events) != 0 {
			return events
		}
	}
	if strings.TrimSpace(result.ConsumerKind) != "" && !ok {
		return notice(surface, "path_picker_consumer_missing", "当前路径选择结果缺少可用的业务处理器，请重新发起或联系维护者检查配置。")
	}
	return notice(surface, "path_picker_cancelled", "已取消路径选择。")
}

func (s *Service) lookupPathPickerConsumer(kind string) (PathPickerConsumer, bool) {
	if s == nil {
		return nil, false
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, false
	}
	consumer := s.pathPickerConsumers[kind]
	return consumer, consumer != nil
}

func canConfirmPathPicker(record *state.ActivePathPickerRecord) bool {
	_, err := confirmedPathPickerSelection(record)
	return err == nil
}

func defaultSelectedPathForMode(mode state.PathPickerMode, currentPath, selectedPath string) string {
	switch mode {
	case state.PathPickerModeDirectory:
		if strings.TrimSpace(selectedPath) != "" {
			return strings.TrimSpace(selectedPath)
		}
		return strings.TrimSpace(currentPath)
	default:
		return strings.TrimSpace(selectedPath)
	}
}

func defaultPathPickerTitle(mode state.PathPickerMode) string {
	switch mode {
	case state.PathPickerModeDirectory:
		return "选择目录"
	default:
		return "选择文件"
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		cloned[key] = strings.TrimSpace(value)
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func statePathPickerMode(mode control.PathPickerMode) (state.PathPickerMode, bool) {
	switch mode {
	case control.PathPickerModeDirectory:
		return state.PathPickerModeDirectory, true
	case control.PathPickerModeFile:
		return state.PathPickerModeFile, true
	default:
		return "", false
	}
}

type resolvedPathPickerTarget struct {
	path string
	kind state.PathPickerMode
}

func resolvePathPickerRoot(rootPath string) (string, error) {
	return state.ResolveWorkspaceRootOnHost(rootPath)
}

func resolvePathPickerInitialState(rootPath string, mode state.PathPickerMode, initialPath string) (string, string, error) {
	if strings.TrimSpace(initialPath) == "" {
		return rootPath, defaultSelectedPathForMode(mode, rootPath, ""), nil
	}
	resolved, err := resolvePathPickerExistingTarget(rootPath, initialPath)
	if err != nil {
		return "", "", err
	}
	switch mode {
	case state.PathPickerModeDirectory:
		if resolved.kind != state.PathPickerModeDirectory {
			return "", "", fmt.Errorf("初始路径必须是目录")
		}
		return resolved.path, resolved.path, nil
	case state.PathPickerModeFile:
		if resolved.kind == state.PathPickerModeFile {
			return filepath.Dir(resolved.path), resolved.path, nil
		}
		return resolved.path, "", nil
	default:
		return "", "", fmt.Errorf("路径选择器模式无效")
	}
}

func resolvePathPickerEntry(rootPath, currentPath, entryName string) (resolvedPathPickerTarget, error) {
	entryName = strings.TrimSpace(entryName)
	if entryName == "" {
		return resolvedPathPickerTarget{}, fmt.Errorf("缺少路径条目")
	}
	return resolvePathPickerExistingTarget(rootPath, filepath.Join(currentPath, entryName))
}

func resolvePathPickerExistingTarget(rootPath, targetPath string) (resolvedPathPickerTarget, error) {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return resolvedPathPickerTarget{}, fmt.Errorf("缺少目标路径")
	}
	absolute, err := filepath.Abs(targetPath)
	if err != nil {
		return resolvedPathPickerTarget{}, err
	}
	absolute = filepath.Clean(absolute)
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return resolvedPathPickerTarget{}, err
	}
	resolved = filepath.Clean(resolved)
	if !pathWithinRoot(rootPath, resolved) {
		return resolvedPathPickerTarget{}, fmt.Errorf("超出允许范围")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return resolvedPathPickerTarget{}, err
	}
	if info.IsDir() {
		return resolvedPathPickerTarget{path: resolved, kind: state.PathPickerModeDirectory}, nil
	}
	return resolvedPathPickerTarget{path: resolved, kind: state.PathPickerModeFile}, nil
}

func pathWithinRoot(rootPath, targetPath string) bool {
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	targetPath = filepath.Clean(strings.TrimSpace(targetPath))
	if rootPath == "" || targetPath == "" {
		return false
	}
	rel, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return false
	}
	return rel == "." || rel == "" || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func samePath(left, right string) bool {
	return filepath.Clean(strings.TrimSpace(left)) == filepath.Clean(strings.TrimSpace(right))
}
