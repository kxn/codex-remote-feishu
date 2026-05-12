package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const defaultPathPickerTTL = 10 * time.Minute

func (s *Service) OpenPathPicker(action control.Action, req control.PathPickerRequest) []eventcontract.Event {
	surface := s.ensureSurface(action)
	return s.openPathPicker(surface, action.ActorUserID, req)
}

func (s *Service) RegisterPathPickerConsumer(kind string, consumer PathPickerConsumer) {
	if s == nil || s.pickers == nil {
		return
	}
	s.pickers.registerPathPickerConsumer(kind, consumer)
}

func (s *Service) RegisterPathPickerEntryFilter(kind string, filter PathPickerEntryFilter) {
	if s == nil || s.pickers == nil {
		return
	}
	s.pickers.registerPathPickerEntryFilter(kind, filter)
}

func (s *Service) openPathPicker(surface *state.SurfaceConsoleRecord, ownerUserID string, req control.PathPickerRequest) []eventcontract.Event {
	return s.openPathPickerWithInline(surface, ownerUserID, req, false)
}

func (s *Service) openPathPickerWithInline(surface *state.SurfaceConsoleRecord, ownerUserID string, req control.PathPickerRequest, inline bool) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	var record *activePathPickerRecord
	return s.openPickerRuntime(
		surface,
		func() error {
			next, err := s.newPathPickerRecord(surface, ownerUserID, req)
			if err != nil {
				return err
			}
			s.setActivePathPicker(surface, next)
			record = next
			return nil
		},
		func() {
			s.clearSurfacePathPicker(surface)
		},
		func(err error) []eventcontract.Event {
			return notice(surface, "path_picker_invalid", err.Error())
		},
		func() (eventcontract.Event, error) {
			return s.buildPathPickerEvent(surface, record, inline)
		},
		func(err error) []eventcontract.Event {
			return notice(surface, "path_picker_invalid", err.Error())
		},
	)
}

func (s *Service) newPathPickerRecord(surface *state.SurfaceConsoleRecord, ownerUserID string, req control.PathPickerRequest) (*activePathPickerRecord, error) {
	mode, ok := runtimePathPickerMode(req.Mode)
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
	return &activePathPickerRecord{
		PickerID:        s.pickers.nextPathPickerToken(),
		MessageID:       strings.TrimSpace(req.SourceMessageID),
		OwnerUserID:     strings.TrimSpace(firstNonEmpty(ownerUserID, surface.ActorUserID)),
		OwnerFlowID:     strings.TrimSpace(req.OwnerFlowID),
		Mode:            mode,
		Title:           strings.TrimSpace(firstNonEmpty(req.Title, defaultPathPickerTitle(mode))),
		StageLabel:      strings.TrimSpace(req.StageLabel),
		Question:        strings.TrimSpace(req.Question),
		RootPath:        rootPath,
		CurrentPath:     currentPath,
		SelectedPath:    selectedPath,
		DirectoryCursor: -1,
		FileCursor:      -1,
		Hint:            strings.TrimSpace(req.Hint),
		ConfirmLabel:    strings.TrimSpace(firstNonEmpty(req.ConfirmLabel, "确认")),
		CancelLabel:     strings.TrimSpace(firstNonEmpty(req.CancelLabel, "取消")),
		CreatedAt:       s.now(),
		ExpiresAt:       expiresAt,
		ConsumerKind:    strings.TrimSpace(req.ConsumerKind),
		ConsumerMeta:    cloneStringMap(req.ConsumerMeta),
		EntryFilterKind: strings.TrimSpace(req.EntryFilterKind),
		EntryFilterMeta: cloneStringMap(req.EntryFilterMeta),
	}, nil
}

func (s *Service) buildPathPickerEvent(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord, inline bool) (eventcontract.Event, error) {
	view, err := s.buildPathPickerView(surface, record)
	if err != nil {
		return eventcontract.Event{}, err
	}
	return s.pathPickerViewEvent(surface, view, inline), nil
}

func (s *Service) handlePathPickerEnter(surface *state.SurfaceConsoleRecord, pickerID, entryName, actorUserID string) []eventcontract.Event {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	resolved, err := resolvePathPickerEntry(record.RootPath, record.CurrentPath, entryName)
	if err != nil {
		return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "目标条目无效", fmt.Sprintf("目标条目无效：%v", err))
	}
	item, allowed := s.filterPathPickerResolvedEntry(surface, record, pathPickerEntryFilterItem(entryName, resolved), resolved.path)
	if !allowed || item.Disabled {
		return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "这个条目当前不可用", pathPickerEntryUnavailableText(item))
	}
	if resolved.kind != pathPickerModeDirectory {
		return s.pathPickerInlineNotice(surface, record, "path_picker_not_directory", "只能进入目录", "只能进入目录。")
	}
	if samePath(record.CurrentPath, resolved.path) {
		return mutatePickerAndRebuild(
			func() {
				record.DirectoryCursor = -1
				record.FileCursor = -1
				clearPathPickerStatus(record)
			},
			func() (eventcontract.Event, error) {
				return s.buildPathPickerEvent(surface, record, true)
			},
			func(err error) []eventcontract.Event {
				return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("目录刷新失败：%v", err))
			},
		)
	}
	return mutatePickerAndRebuild(
		func() {
			record.CurrentPath = resolved.path
			record.SelectedPath = defaultSelectedPathForMode(record.Mode, record.CurrentPath, "")
			record.DirectoryCursor = -1
			record.FileCursor = -1
			clearPathPickerStatus(record)
		},
		func() (eventcontract.Event, error) {
			return s.buildPathPickerEvent(surface, record, true)
		},
		func(err error) []eventcontract.Event {
			return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("目录刷新失败：%v", err))
		},
	)
}

func (s *Service) handlePathPickerUp(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) []eventcontract.Event {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	if samePath(record.CurrentPath, record.RootPath) {
		return mutatePickerAndRebuild(
			func() {
				clearPathPickerStatus(record)
			},
			func() (eventcontract.Event, error) {
				return s.buildPathPickerEvent(surface, record, true)
			},
			func(err error) []eventcontract.Event {
				return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "目录刷新失败", fmt.Sprintf("目录刷新失败：%v", err))
			},
		)
	}
	parent := filepath.Dir(record.CurrentPath)
	resolved, err := resolvePathPickerExistingTarget(record.RootPath, parent)
	if err != nil {
		return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "无法返回上一级", fmt.Sprintf("无法返回上一级：%v", err))
	}
	if resolved.kind != pathPickerModeDirectory {
		return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "上一级目录无效", "上一级目录无效。")
	}
	return mutatePickerAndRebuild(
		func() {
			record.CurrentPath = resolved.path
			record.SelectedPath = defaultSelectedPathForMode(record.Mode, record.CurrentPath, "")
			record.DirectoryCursor = -1
			record.FileCursor = -1
			clearPathPickerStatus(record)
		},
		func() (eventcontract.Event, error) {
			return s.buildPathPickerEvent(surface, record, true)
		},
		func(err error) []eventcontract.Event {
			return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "目录刷新失败", fmt.Sprintf("目录刷新失败：%v", err))
		},
	)
}

func (s *Service) handlePathPickerSelect(surface *state.SurfaceConsoleRecord, pickerID, entryName, actorUserID string) []eventcontract.Event {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	resolved, err := resolvePathPickerEntry(record.RootPath, record.CurrentPath, entryName)
	if err != nil {
		return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "目标条目无效", fmt.Sprintf("目标条目无效：%v", err))
	}
	item, allowed := s.filterPathPickerResolvedEntry(surface, record, pathPickerEntryFilterItem(entryName, resolved), resolved.path)
	if !allowed || item.Disabled {
		return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "这个条目当前不可用", pathPickerEntryUnavailableText(item))
	}
	switch record.Mode {
	case pathPickerModeFile:
		if resolved.kind != pathPickerModeFile {
			return s.pathPickerInlineNotice(surface, record, "path_picker_not_file", "当前只可选择文件", "当前只可选择文件。")
		}
	case pathPickerModeDirectory:
		if resolved.kind != pathPickerModeDirectory {
			return s.pathPickerInlineNotice(surface, record, "path_picker_not_directory", "当前只可选择目录", "当前只可选择目录。")
		}
	}
	return mutatePickerAndRebuild(
		func() {
			record.SelectedPath = resolved.path
			clearPathPickerStatus(record)
		},
		func() (eventcontract.Event, error) {
			return s.buildPathPickerEvent(surface, record, true)
		},
		func(err error) []eventcontract.Event {
			return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "目录刷新失败", fmt.Sprintf("目录刷新失败：%v", err))
		},
	)
}

func (s *Service) handlePathPickerPage(surface *state.SurfaceConsoleRecord, pickerID, fieldName string, cursor int, actorUserID string) []eventcontract.Event {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	entries, err := s.buildPathPickerEntries(surface, record)
	if err != nil {
		return s.pathPickerInlineNotice(surface, record, "path_picker_invalid_entry", "目录刷新失败", fmt.Sprintf("目录刷新失败：%v", err))
	}
	switch strings.TrimSpace(fieldName) {
	case frontstagecontract.CardPathPickerDirectorySelectFieldName:
		record.DirectoryCursor = normalizePathPickerDropdownCursor(cursor, len(pathPickerEntriesByKind(entries, control.PathPickerEntryDirectory)))
	case frontstagecontract.CardPathPickerFileSelectFieldName:
		if record.Mode != pathPickerModeFile {
			return notice(surface, "path_picker_invalid_page_action", "当前翻页动作无效，请重新打开路径选择器。")
		}
		record.FileCursor = normalizePathPickerDropdownCursor(cursor, len(pathPickerEntriesByKind(entries, control.PathPickerEntryFile)))
		record.SelectedPath = ""
	default:
		return notice(surface, "path_picker_invalid_page_action", "当前翻页动作无效，请重新打开路径选择器。")
	}
	return mutatePickerAndRebuild(
		func() {
			clearPathPickerStatus(record)
		},
		func() (eventcontract.Event, error) {
			return s.buildPathPickerEvent(surface, record, true)
		},
		func(err error) []eventcontract.Event {
			return notice(surface, "path_picker_invalid_entry", fmt.Sprintf("目录刷新失败：%v", err))
		},
	)
}

func (s *Service) handlePathPickerConfirm(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) []eventcontract.Event {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	selectedPath, err := confirmedPathPickerSelection(record)
	if err != nil {
		return s.pathPickerNotice(surface, record, "path_picker_selection_missing", "当前还不能确认", err.Error(), false)
	}
	resolved, err := resolvePathPickerExistingTarget(record.RootPath, selectedPath)
	if err != nil {
		return s.pathPickerNotice(surface, record, "path_picker_invalid_entry", "目标条目无效", fmt.Sprintf("目标条目无效：%v", err), false)
	}
	item, allowed := s.filterPathPickerResolvedEntry(surface, record, pathPickerEntryFilterItem(pathPickerEntryNameForPath(selectedPath), resolved), resolved.path)
	if !allowed || item.Disabled {
		return s.pathPickerNotice(surface, record, "path_picker_invalid_entry", "这个条目当前不可用", pathPickerEntryUnavailableText(item), false)
	}
	result := pathPickerResultFromRecord(record, selectedPath)
	if consumer, ok := s.lookupPathPickerConsumer(result.ConsumerKind); ok {
		if owner, ok := consumer.(PathPickerConfirmLifecycleOwner); ok && owner.PathPickerOwnsConfirmLifecycle() {
			return consumer.PathPickerConfirmed(s, surface, result)
		}
	}
	return s.dispatchPathPickerConfirmed(surface, record, result)
}

func (s *Service) handlePathPickerCancel(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) []eventcontract.Event {
	record, blocked := s.requireActivePathPicker(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	result := pathPickerResultFromRecord(record, currentSelectedPath(record))
	return s.dispatchPathPickerCancelled(surface, record, result)
}

func (s *Service) requireActivePathPicker(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) (*activePathPickerRecord, []eventcontract.Event) {
	if surface == nil || s.activePathPicker(surface) == nil {
		return nil, notice(surface, "path_picker_expired", "这个路径选择器已失效，请重新发起。")
	}
	record := s.activePathPicker(surface)
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(s.now()) {
		s.clearSurfacePathPicker(surface)
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

func (s *Service) buildPathPickerView(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord) (control.FeishuPathPickerView, error) {
	if record == nil {
		return control.FeishuPathPickerView{}, fmt.Errorf("路径选择器不存在")
	}
	current, err := resolvePathPickerExistingTarget(record.RootPath, record.CurrentPath)
	if err != nil {
		return control.FeishuPathPickerView{}, err
	}
	if current.kind != pathPickerModeDirectory {
		return control.FeishuPathPickerView{}, fmt.Errorf("当前路径不是目录")
	}
	view := control.FeishuPathPickerView{
		PickerID:       record.PickerID,
		Mode:           control.PathPickerMode(record.Mode),
		Title:          strings.TrimSpace(record.Title),
		StageLabel:     strings.TrimSpace(record.StageLabel),
		Question:       strings.TrimSpace(record.Question),
		RootPath:       record.RootPath,
		CurrentPath:    current.path,
		SelectedPath:   currentSelectedPath(record),
		BodySections:   pathPickerBodySections(record.RootPath, current.path, currentSelectedPath(record)),
		NoticeSections: pathPickerStatusNoticeSections(record.StatusTitle, record.StatusText, record.StatusSections, record.StatusFooter),
		ConfirmLabel:   strings.TrimSpace(firstNonEmpty(record.ConfirmLabel, "确认")),
		CancelLabel:    strings.TrimSpace(firstNonEmpty(record.CancelLabel, "取消")),
		CanGoUp:        !samePath(current.path, record.RootPath),
		CanConfirm:     canConfirmPathPicker(record),
	}
	entries, err := s.buildPathPickerEntries(surface, record)
	if err != nil {
		return control.FeishuPathPickerView{}, err
	}
	directoryCursor := record.DirectoryCursor
	if directoryCursor < 0 {
		directoryCursor = 0
	}
	directoryCursor = normalizePathPickerDropdownCursor(directoryCursor, len(pathPickerEntriesByKind(entries, control.PathPickerEntryDirectory)))
	record.DirectoryCursor = directoryCursor
	fileCursor := record.FileCursor
	if fileCursor < 0 {
		fileCursor = pathPickerEntryIndexByKind(entries, control.PathPickerEntryFile, view.SelectedPath)
	}
	fileCursor = normalizePathPickerDropdownCursor(fileCursor, len(pathPickerEntriesByKind(entries, control.PathPickerEntryFile)))
	record.FileCursor = fileCursor
	view.Entries = entries
	view.DirectoryCursor = directoryCursor
	view.FileCursor = fileCursor
	if len(entries) == 0 && strings.TrimSpace(record.StageLabel) == "" && strings.TrimSpace(record.Question) == "" {
		view.Hint = "当前目录为空。"
	}
	if record.Mode == pathPickerModeFile && view.SelectedPath == "" {
		view.Hint = strings.TrimSpace(firstNonEmpty(view.Hint, "请选择一个文件后再确认。"))
	}
	if extraHint := strings.TrimSpace(record.Hint); extraHint != "" {
		if strings.TrimSpace(view.Hint) == "" {
			view.Hint = extraHint
		} else {
			view.Hint = strings.TrimSpace(view.Hint) + "\n" + extraHint
		}
	}
	return control.NormalizeFeishuPathPickerView(view), nil
}

func (s *Service) buildPathPickerEntries(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord) ([]control.FeishuPathPickerEntry, error) {
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
		resolved, err := resolvePathPickerEntry(record.RootPath, record.CurrentPath, name)
		if err != nil {
			item := control.FeishuPathPickerEntry{Name: name, Label: name}
			item.Disabled = true
			item.DisabledReason = err.Error()
			items = append(items, item)
			continue
		}
		item, allowed := s.filterPathPickerResolvedEntry(surface, record, pathPickerEntryViewItem(record, name, resolved), resolved.path)
		if !allowed {
			continue
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

func pathPickerEntryViewItem(record *activePathPickerRecord, name string, resolved resolvedPathPickerTarget) control.FeishuPathPickerEntry {
	item := pathPickerEntryFilterItem(name, resolved)
	switch resolved.kind {
	case pathPickerModeDirectory:
		item.ActionKind = control.PathPickerEntryActionEnter
		item.Selected = samePath(currentSelectedPath(record), resolved.path)
	case pathPickerModeFile:
		if record != nil && record.Mode == pathPickerModeFile {
			item.ActionKind = control.PathPickerEntryActionSelect
			item.Selected = samePath(strings.TrimSpace(record.SelectedPath), resolved.path)
		} else {
			item.Disabled = true
			item.DisabledReason = "当前只可选择目录"
		}
	}
	return item
}

func pathPickerEntryFilterItem(name string, resolved resolvedPathPickerTarget) control.FeishuPathPickerEntry {
	item := control.FeishuPathPickerEntry{Name: strings.TrimSpace(name), Label: strings.TrimSpace(name)}
	switch resolved.kind {
	case pathPickerModeDirectory:
		item.Kind = control.PathPickerEntryDirectory
	case pathPickerModeFile:
		item.Kind = control.PathPickerEntryFile
	}
	return item
}

func pathPickerEntryNameForPath(path string) string {
	name := strings.TrimSpace(filepath.Base(strings.TrimSpace(path)))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return strings.TrimSpace(path)
	}
	return name
}

func pathPickerEntryUnavailableText(item control.FeishuPathPickerEntry) string {
	if text := strings.TrimSpace(item.DisabledReason); text != "" {
		return text
	}
	return "这个条目当前不可用，请刷新后重试。"
}

func (s *Service) filterPathPickerResolvedEntry(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord, item control.FeishuPathPickerEntry, resolvedPath string) (control.FeishuPathPickerEntry, bool) {
	if s == nil || record == nil {
		return item, true
	}
	filter, ok := s.lookupPathPickerEntryFilter(record.EntryFilterKind)
	if !ok {
		return item, true
	}
	return filter.PathPickerFilterEntry(s, surface, record, item, resolvedPath)
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

func (s *Service) pruneExpiredPathPicker(surface *state.SurfaceConsoleRecord) {
	if s == nil || surface == nil || s.activePathPicker(surface) == nil {
		return
	}
	expiresAt := s.activePathPicker(surface).ExpiresAt
	if expiresAt.IsZero() || expiresAt.After(s.now()) {
		return
	}
	s.clearSurfacePathPicker(surface)
}

func confirmedPathPickerSelection(record *activePathPickerRecord) (string, error) {
	selectedPath := currentSelectedPath(record)
	if strings.TrimSpace(selectedPath) == "" {
		switch record.Mode {
		case pathPickerModeDirectory:
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
	case pathPickerModeDirectory:
		if resolved.kind != pathPickerModeDirectory {
			return "", fmt.Errorf("当前只可确认目录。")
		}
	case pathPickerModeFile:
		if resolved.kind != pathPickerModeFile {
			return "", fmt.Errorf("当前只可确认文件。")
		}
	}
	return resolved.path, nil
}

func currentSelectedPath(record *activePathPickerRecord) string {
	if record == nil {
		return ""
	}
	if record.Mode == pathPickerModeDirectory {
		if strings.TrimSpace(record.SelectedPath) != "" {
			return strings.TrimSpace(record.SelectedPath)
		}
		return strings.TrimSpace(record.CurrentPath)
	}
	return strings.TrimSpace(record.SelectedPath)
}

func pathPickerResultFromRecord(record *activePathPickerRecord, selectedPath string) control.PathPickerResult {
	if record == nil {
		return control.PathPickerResult{}
	}
	return control.PathPickerResult{
		PickerID:     strings.TrimSpace(record.PickerID),
		MessageID:    strings.TrimSpace(record.MessageID),
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

func (s *Service) dispatchPathPickerConfirmed(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord, result control.PathPickerResult) []eventcontract.Event {
	consumer, ok := s.lookupPathPickerConsumer(result.ConsumerKind)
	if ok {
		if events := consumer.PathPickerConfirmed(s, surface, result); len(events) != 0 {
			filtered := pathPickerFilteredFollowupEvents(events)
			if len(filtered) != 0 {
				s.clearSurfacePathPicker(surface)
				return events
			}
			return s.finishPathPickerWithStatus(surface, record, frontstagecontract.PhaseSucceeded, "已确认路径", firstNonEmpty(pathPickerFirstNoticeText(events), fmt.Sprintf("已确认路径：`%s`。", result.SelectedPath)), nil, "", false, nil)
		}
	}
	if strings.TrimSpace(result.ConsumerKind) != "" && !ok {
		return s.finishPathPickerWithStatus(surface, record, frontstagecontract.PhaseFailed, "当前路径处理器不可用", "当前路径选择结果缺少可用的业务处理器，请重新发起或联系维护者检查配置。", nil, "", false, nil)
	}
	return s.finishPathPickerWithStatus(surface, record, frontstagecontract.PhaseSucceeded, "已确认路径", fmt.Sprintf("已确认路径：`%s`。", result.SelectedPath), nil, "", false, nil)
}

func (s *Service) dispatchPathPickerCancelled(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord, result control.PathPickerResult) []eventcontract.Event {
	consumer, ok := s.lookupPathPickerConsumer(result.ConsumerKind)
	if ok {
		if events := consumer.PathPickerCancelled(s, surface, result); len(events) != 0 {
			filtered := pathPickerFilteredFollowupEvents(events)
			if len(filtered) != 0 {
				s.clearSurfacePathPicker(surface)
				return events
			}
			return s.finishPathPickerWithStatus(surface, record, frontstagecontract.PhaseCancelled, "已取消路径选择", firstNonEmpty(pathPickerFirstNoticeText(events), "已取消路径选择。"), nil, "", false, nil)
		}
	}
	if strings.TrimSpace(result.ConsumerKind) != "" && !ok {
		return s.finishPathPickerWithStatus(surface, record, frontstagecontract.PhaseFailed, "当前路径处理器不可用", "当前路径选择结果缺少可用的业务处理器，请重新发起或联系维护者检查配置。", nil, "", false, nil)
	}
	return s.finishPathPickerWithStatus(surface, record, frontstagecontract.PhaseCancelled, "已取消路径选择", "已取消路径选择。", nil, "", false, nil)
}

func (s *Service) lookupPathPickerConsumer(kind string) (PathPickerConsumer, bool) {
	if s == nil || s.pickers == nil {
		return nil, false
	}
	return s.pickers.lookupPathPickerConsumer(kind)
}

func (s *Service) lookupPathPickerEntryFilter(kind string) (PathPickerEntryFilter, bool) {
	if s == nil || s.pickers == nil {
		return nil, false
	}
	return s.pickers.lookupPathPickerEntryFilter(kind)
}

func (s *Service) RecordPathPickerMessage(surfaceID, pickerID, messageID string) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	record := s.activePathPicker(surface)
	if surface == nil || record == nil {
		return
	}
	if strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		return
	}
	record.MessageID = strings.TrimSpace(messageID)
}

func canConfirmPathPicker(record *activePathPickerRecord) bool {
	_, err := confirmedPathPickerSelection(record)
	return err == nil
}

func defaultSelectedPathForMode(mode pathPickerMode, currentPath, selectedPath string) string {
	switch mode {
	case pathPickerModeDirectory:
		if strings.TrimSpace(selectedPath) != "" {
			return strings.TrimSpace(selectedPath)
		}
		return strings.TrimSpace(currentPath)
	default:
		return strings.TrimSpace(selectedPath)
	}
}

func defaultPathPickerTitle(mode pathPickerMode) string {
	switch mode {
	case pathPickerModeDirectory:
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

func runtimePathPickerMode(mode control.PathPickerMode) (pathPickerMode, bool) {
	switch mode {
	case control.PathPickerModeDirectory:
		return pathPickerModeDirectory, true
	case control.PathPickerModeFile:
		return pathPickerModeFile, true
	default:
		return "", false
	}
}

type resolvedPathPickerTarget struct {
	path string
	kind pathPickerMode
}

func resolvePathPickerRoot(rootPath string) (string, error) {
	return state.ResolveWorkspaceRootOnHost(rootPath)
}

func resolvePathPickerInitialState(rootPath string, mode pathPickerMode, initialPath string) (string, string, error) {
	if strings.TrimSpace(initialPath) == "" {
		return rootPath, defaultSelectedPathForMode(mode, rootPath, ""), nil
	}
	resolved, err := resolvePathPickerExistingTarget(rootPath, initialPath)
	if err != nil {
		return "", "", err
	}
	switch mode {
	case pathPickerModeDirectory:
		if resolved.kind != pathPickerModeDirectory {
			return "", "", fmt.Errorf("初始路径必须是目录")
		}
		return resolved.path, resolved.path, nil
	case pathPickerModeFile:
		if resolved.kind == pathPickerModeFile {
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
		return resolvedPathPickerTarget{path: resolved, kind: pathPickerModeDirectory}, nil
	}
	return resolvedPathPickerTarget{path: resolved, kind: pathPickerModeFile}, nil
}

func pathWithinRoot(rootPath, targetPath string) bool {
	rootPath = canonicalPathPickerPath(rootPath)
	targetPath = canonicalPathPickerPath(targetPath)
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
	return canonicalPathPickerPath(left) == canonicalPathPickerPath(right)
}

func canonicalPathPickerPath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return ""
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = filepath.Clean(absolute)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
