package orchestrator

import (
	"regexp"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	defaultReviewCommitPickerTTL = 10 * time.Minute
	reviewCommitPickerLimit      = 10
	reviewCommitPickerFieldName  = "commit_sha"
)

var reviewCommitCandidatePattern = regexp.MustCompile(`(?i)\b[0-9a-f]{7,40}\b`)

func (s *Service) openReviewCommitPicker(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if s.activeReviewSession(surface) != nil {
		return notice(surface, "review_session_active", "当前已经在审阅中；请直接继续提问，或使用“放弃审阅”/“按审阅意见继续修改”退出。")
	}
	entry := s.resolveReviewEntryFromCurrentContext(surface, action)
	if !entry.Ready {
		return notice(surface, entry.FailureCode, entry.FailureText)
	}
	commits, code, text := s.listRecentReviewCommits(entry.ThreadCWD)
	if code != "" {
		return notice(surface, code, text)
	}
	now := s.now()
	flow := newOwnerCardFlowRecord(
		ownerCardFlowKindReviewPicker,
		s.pickers.nextReviewPickerToken(),
		firstNonEmpty(surface.ActorUserID, action.ActorUserID),
		now,
		defaultReviewCommitPickerTTL,
		ownerCardFlowPhaseEditing,
	)
	record := &activeReviewPickerRecord{
		InstanceID:     strings.TrimSpace(entry.InstanceID),
		ParentThreadID: strings.TrimSpace(entry.ParentThreadID),
		ThreadCWD:      strings.TrimSpace(entry.ThreadCWD),
		RecentCommits:  append([]gitmeta.CommitSummary(nil), commits...),
		CreatedAt:      now,
		ExpiresAt:      now.Add(defaultReviewCommitPickerTTL),
	}
	s.setActiveOwnerCardFlow(surface, flow)
	s.setActiveReviewPicker(surface, record)
	return []eventcontract.Event{s.pageEvent(surface, s.buildReviewCommitPickerView(flow, record))}
}

func (s *Service) cancelReviewCommitPicker(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	flow, record, code, text := s.requireActiveReviewCommitPicker(surface, action.MessageID, action.ActorUserID, action.IsCardAction())
	if flow == nil || record == nil {
		return notice(surface, code, text)
	}
	s.clearReviewCommitPickerRuntime(surface)
	return notice(surface, "review_commit_picker_cancelled", "已取消提交记录审阅选择。")
}

func (s *Service) resolveCommitReviewStartFromCurrentContext(surface *state.SurfaceConsoleRecord, action control.Action, commitSHA string) reviewStartState {
	return s.resolveCommitReviewStartFromEntry(s.resolveReviewEntryFromCurrentContext(surface, action), commitSHA)
}

func (s *Service) resolveCommitReviewStartFromFinalCard(surface *state.SurfaceConsoleRecord, action control.Action, commitSHA string) reviewStartState {
	return s.resolveCommitReviewStartFromEntry(s.resolveReviewEntryFromFinalCard(surface, action), commitSHA)
}

func (s *Service) resolveCommitReviewStartFromActivePicker(surface *state.SurfaceConsoleRecord, action control.Action, commitSHA string) (reviewStartState, bool) {
	if surface == nil {
		return failedReviewStart("", ""), false
	}
	if !s.activeReviewCommitPickerMatchesMessage(surface, action.MessageID, action.IsCardAction()) {
		return failedReviewStart("", ""), false
	}
	flow, record, code, text := s.requireActiveReviewCommitPicker(surface, action.MessageID, action.ActorUserID, action.IsCardAction())
	if flow == nil || record == nil {
		return failedReviewStart(code, text), true
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != strings.TrimSpace(record.InstanceID) {
		s.clearReviewCommitPickerRuntime(surface)
		return failedReviewStart("review_source_instance_changed", "当前已经切换到其他实例，请重新发送 `/review commit`。"), true
	}
	inst := s.root.Instances[strings.TrimSpace(record.InstanceID)]
	if inst == nil {
		s.clearReviewCommitPickerRuntime(surface)
		return failedReviewStart("review_instance_missing", "当前实例已经不可用，请稍后重试。"), true
	}
	thread := inst.Threads[strings.TrimSpace(record.ParentThreadID)]
	if !threadVisible(thread) {
		s.clearReviewCommitPickerRuntime(surface)
		return failedReviewStart("review_parent_thread_missing", "原始会话已经不可用，请重新发送 `/review commit`。"), true
	}
	entry := s.resolveReviewEntryFromThread(
		record.InstanceID,
		record.ParentThreadID,
		firstNonEmpty(strings.TrimSpace(thread.CWD), strings.TrimSpace(record.ThreadCWD), strings.TrimSpace(inst.WorkspaceRoot)),
		strings.TrimSpace(action.MessageID),
	)
	return s.resolveCommitReviewStartFromEntry(entry, commitSHA), true
}

func (s *Service) resolveCommitReviewStartFromEntry(entry reviewEntryContext, commitSHA string) reviewStartState {
	if !entry.Ready {
		return failedReviewStart(entry.FailureCode, entry.FailureText)
	}
	info, err := gitmeta.InspectWorkspace(entry.ThreadCWD, gitmeta.InspectOptions{})
	if err != nil {
		return failedReviewStart("review_git_state_unavailable", "当前无法检查工作区的 Git 状态，请稍后重试。")
	}
	if !info.InRepo() {
		return failedReviewStart("review_not_in_repo", "当前会话工作目录不在 Git 仓库内，无法按提交记录发起审阅。")
	}
	resolved, err := gitmeta.ResolveCommitPrefix(entry.ThreadCWD, commitSHA)
	if err != nil {
		return failedReviewStart("review_commit_unavailable", "当前无法解析这个提交记录，请稍后重试。")
	}
	switch resolved.Status {
	case gitmeta.CommitResolveNotFound:
		return failedReviewStart("review_commit_not_found", "当前仓库里找不到这个提交记录，请确认 SHA 是否正确。")
	case gitmeta.CommitResolveAmbiguous:
		return failedReviewStart("review_commit_ambiguous", "当前提交前缀匹配到多个提交记录，请改用更长的 SHA。")
	case gitmeta.CommitResolveFound:
		commit := resolved.Commit.Normalized()
		return reviewStartFromEntry(entry, agentproto.ReviewTarget{
			Kind:        agentproto.ReviewTargetKindCommit,
			CommitSHA:   commit.SHA,
			CommitTitle: commit.Subject,
		}, reviewCommitTargetLabel(commit))
	default:
		return failedReviewStart("review_commit_unavailable", "当前无法解析这个提交记录，请稍后重试。")
	}
}

func (s *Service) listRecentReviewCommits(cwd string) ([]gitmeta.CommitSummary, string, string) {
	info, err := gitmeta.InspectWorkspace(cwd, gitmeta.InspectOptions{})
	if err != nil {
		return nil, "review_git_state_unavailable", "当前无法检查工作区的 Git 状态，请稍后重试。"
	}
	if !info.InRepo() {
		return nil, "review_not_in_repo", "当前会话工作目录不在 Git 仓库内，无法按提交记录发起审阅。"
	}
	commits, err := gitmeta.ListRecentCommits(cwd, reviewCommitPickerLimit)
	if err != nil {
		return nil, "review_commit_unavailable", "当前无法读取最近的提交记录，请稍后重试。"
	}
	if len(commits) == 0 {
		return nil, "review_no_commits", "当前 Git 仓库还没有可审阅的提交记录。"
	}
	return commits, "", ""
}

func reviewCommitTargetLabel(commit gitmeta.CommitSummary) string {
	short := reviewShortCommitSHA(commit.ShortSHA)
	if short == "" {
		short = reviewShortCommitSHA(commit.SHA)
	}
	if short == "" {
		return "提交"
	}
	return "提交 " + short
}

func reviewShortCommitSHA(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch {
	case len(value) >= 7:
		return value[:7]
	default:
		return value
	}
}

func reviewCommitOptionLabel(commit gitmeta.CommitSummary) string {
	commit = commit.Normalized()
	label := reviewShortCommitSHA(firstNonEmpty(commit.ShortSHA, commit.SHA))
	subject := strings.TrimSpace(commit.Subject)
	if subject == "" {
		return label
	}
	if label == "" {
		return subject
	}
	return label + " " + subject
}

func (s *Service) buildReviewCommitPickerView(flow *activeOwnerCardFlowRecord, record *activeReviewPickerRecord) control.FeishuPageView {
	options := make([]control.CommandCatalogFormFieldOption, 0, len(record.RecentCommits))
	for _, commit := range record.RecentCommits {
		commit = commit.Normalized()
		if commit.SHA == "" {
			continue
		}
		options = append(options, control.CommandCatalogFormFieldOption{
			Label: reviewCommitOptionLabel(commit),
			Value: commit.SHA,
		})
	}
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		CommandID:   control.FeishuCommandReview,
		Title:       "选择提交记录",
		Interactive: true,
		TrackingKey: strings.TrimSpace(flow.FlowID),
		Sections: []control.CommandCatalogSection{{
			Title: "",
			Entries: []control.CommandCatalogEntry{{
				Form: &control.CommandCatalogForm{
					CommandID:   control.FeishuCommandReview,
					SubmitValue: frontstagecontract.ActionPayloadPageLocalSubmit(string(control.ActionReviewCommand), "commit", reviewCommitPickerFieldName),
					SubmitLabel: "开始审阅",
					Field: control.CommandCatalogFormField{
						Name:        reviewCommitPickerFieldName,
						Kind:        control.CommandCatalogFormFieldSelectStatic,
						Placeholder: "请选择最近 10 条提交记录",
						Options:     options,
					},
				},
			}},
		}},
		RelatedButtons: []control.CommandCatalogButton{{
			Label:         "取消",
			Kind:          control.CommandCatalogButtonCallbackAction,
			CallbackValue: frontstagecontract.ActionPayloadPageLocalAction(string(control.ActionReviewCommand), "cancel"),
		}},
	})
}

func (s *Service) activeReviewCommitPickerMatchesMessage(surface *state.SurfaceConsoleRecord, messageID string, fromCardAction bool) bool {
	if surface == nil {
		return false
	}
	flow := s.activeOwnerCardFlow(surface)
	record := s.activeReviewPicker(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindReviewPicker || record == nil {
		return false
	}
	if !fromCardAction {
		return true
	}
	return strings.TrimSpace(flow.MessageID) != "" && strings.TrimSpace(flow.MessageID) == strings.TrimSpace(messageID)
}

func (s *Service) requireActiveReviewCommitPicker(surface *state.SurfaceConsoleRecord, messageID, actorUserID string, fromCardAction bool) (*activeOwnerCardFlowRecord, *activeReviewPickerRecord, string, string) {
	if surface == nil {
		return nil, nil, "review_commit_picker_missing", "当前没有进行中的提交记录选择卡片。"
	}
	flow := s.activeOwnerCardFlow(surface)
	record := s.activeReviewPicker(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindReviewPicker || record == nil {
		return nil, nil, "review_commit_picker_missing", "当前没有进行中的 commit 选择卡片。"
	}
	if !flow.ExpiresAt.IsZero() && !flow.ExpiresAt.After(s.now()) {
		s.clearReviewCommitPickerRuntime(surface)
		return nil, nil, "review_commit_picker_expired", "这张 commit 选择卡片已失效，请重新发送 `/review commit`。"
	}
	actorUserID = strings.TrimSpace(firstNonEmpty(actorUserID, surface.ActorUserID))
	if ownerUserID := strings.TrimSpace(flow.OwnerUserID); ownerUserID != "" && actorUserID != "" && ownerUserID != actorUserID {
		return nil, nil, "review_commit_picker_unauthorized", "这张 commit 选择卡片只允许发起者本人操作。"
	}
	if fromCardAction {
		if strings.TrimSpace(flow.MessageID) == "" || strings.TrimSpace(flow.MessageID) != strings.TrimSpace(messageID) {
			return nil, nil, "review_commit_picker_expired", "这张 commit 选择卡片已失效，请重新发送 `/review commit`。"
		}
	}
	return flow, record, "", ""
}

func (s *Service) clearReviewCommitPickerRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	s.clearSurfaceReviewPicker(surface)
	flow := s.activeOwnerCardFlow(surface)
	if flow != nil && flow.Kind == ownerCardFlowKindReviewPicker {
		s.clearSurfaceOwnerCardFlow(surface)
	}
}

func (s *Service) ResolveFinalBlockCommitReviewTargets(surfaceID string, block render.Block, rawBody string) []gitmeta.CommitSummary {
	rawBody = strings.TrimSpace(rawBody)
	if rawBody == "" {
		return nil
	}
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if !s.reviewCommandAllowedForSurface(surface, block.InstanceID) {
		return nil
	}
	entry := s.resolveReviewEntryForBlock(surfaceID, block)
	if !entry.Ready {
		return nil
	}
	commits, code, _ := s.listRecentReviewCommits(entry.ThreadCWD)
	if code != "" || len(commits) == 0 {
		return nil
	}
	candidates := reviewCommitCandidatePattern.FindAllString(rawBody, -1)
	if len(candidates) == 0 {
		return nil
	}
	seenPrefix := map[string]bool{}
	seenCommit := map[string]bool{}
	result := make([]gitmeta.CommitSummary, 0, len(candidates))
	for _, candidate := range candidates {
		prefix := strings.TrimSpace(strings.ToLower(candidate))
		if prefix == "" || seenPrefix[prefix] {
			continue
		}
		seenPrefix[prefix] = true
		match, ok := gitmeta.MatchRecentCommitPrefix(commits, prefix)
		if !ok {
			continue
		}
		match = match.Normalized()
		if match.SHA == "" || seenCommit[match.SHA] {
			continue
		}
		seenCommit[match.SHA] = true
		result = append(result, match)
	}
	return result
}
