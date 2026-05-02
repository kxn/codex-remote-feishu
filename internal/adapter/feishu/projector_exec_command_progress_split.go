package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type execProgressRenderedLine struct {
	ID                string
	Kind              string
	Status            string
	Seq               int
	Content           string
	Transient         bool
	CarryoverEligible bool
}

type execProgressCardWindowState struct {
	NewCard  bool
	StartSeq int
	EndSeq   int
	Lines    []execProgressRenderedLine
}

func execCommandProgressRenderedLines(progress control.ExecCommandProgress) []execProgressRenderedLine {
	items := normalizedExecProgressTimeline(progress)
	verbose := strings.EqualFold(strings.TrimSpace(progress.Verbosity), "verbose")
	fileLabels := execProgressFileChangeDisplayLabels(items)
	lines := make([]execProgressRenderedLine, 0, len(items))
	for _, item := range items {
		content := renderExecProgressTimelineItem(item, verbose, fileLabels)
		if strings.TrimSpace(content) == "" {
			continue
		}
		lines = append(lines, execProgressRenderedLine{
			ID:                item.ID,
			Kind:              item.Kind,
			Status:            item.Status,
			Seq:               item.LastSeq,
			Content:           content,
			CarryoverEligible: execProgressTimelineItemCanCarryOver(item),
		})
	}
	return lines
}

func execProgressTimelineItemCanCarryOver(item control.ExecCommandProgressTimelineItem) bool {
	switch strings.ToLower(strings.TrimSpace(item.Status)) {
	case "running", "started":
	default:
		return false
	}
	return true
}

func normalizeExecProgressCardStartSeq(progress control.ExecCommandProgress, lines []execProgressRenderedLine) int {
	activeStartSeq := activeExecCommandProgressSegmentStartSeq(progress)
	firstPersistent := 0
	for _, line := range lines {
		if line.Transient || line.Seq <= 0 {
			continue
		}
		if firstPersistent == 0 {
			firstPersistent = line.Seq
		}
		if activeStartSeq > 0 && line.Seq >= activeStartSeq {
			return activeStartSeq
		}
	}
	if firstPersistent > 0 {
		return firstPersistent
	}
	if activeStartSeq > 0 {
		return activeStartSeq
	}
	return 1
}

func activeExecCommandProgressSegmentStartSeq(progress control.ExecCommandProgress) int {
	if strings.TrimSpace(progress.ActiveSegmentID) != "" {
		for _, segment := range progress.Segments {
			if strings.TrimSpace(segment.SegmentID) != strings.TrimSpace(progress.ActiveSegmentID) {
				continue
			}
			if segment.StartSeq > 0 {
				return segment.StartSeq
			}
		}
	}
	if len(progress.Segments) == 0 {
		return 0
	}
	return progress.Segments[len(progress.Segments)-1].StartSeq
}

func execProgressRenderedContent(lines []execProgressRenderedLine) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, line.Content)
	}
	return out
}

func execProgressRenderedElements(lines []execProgressRenderedLine) []map[string]any {
	return execCommandProgressElements(execProgressRenderedContent(lines))
}

func execProgressCardFits(lines []execProgressRenderedLine, subtitle string) bool {
	if len(lines) == 0 {
		return true
	}
	op := Operation{
		Kind:            OperationSendCard,
		CardTitle:       "工作中",
		CardSubtitle:    subtitle,
		CardSubtitleTag: cardTextTagLarkMarkdown,
		CardThemeKey:    cardThemeProgress,
		CardElements:    execProgressRenderedElements(lines),
		CardUpdateMulti: true,
		cardEnvelope:    cardEnvelopeV2,
		card:            rawCardDocumentWithHeader("工作中", cardTextTagPlainText, subtitle, cardTextTagLarkMarkdown, "", cardThemeProgress, execProgressRenderedElements(lines)),
	}
	payload := renderOperationCard(op, op.effectiveCardEnvelope())
	return feishuInteractiveMessageTransportFits(payload)
}

func execProgressCardWindow(progress control.ExecCommandProgress, lines []execProgressRenderedLine) execProgressCardWindowState {
	subtitle := detourHeaderSubtitle(progress.DetourLabel)
	startSeq := normalizeExecProgressCardStartSeq(progress, lines)
	persistent := make([]execProgressRenderedLine, 0, len(lines))
	transient := make([]execProgressRenderedLine, 0, 1)
	for _, line := range lines {
		if line.Transient {
			transient = append(transient, line)
			continue
		}
		persistent = append(persistent, line)
	}
	if len(persistent) == 0 {
		if len(transient) == 0 || !execProgressCardFits(transient, subtitle) {
			return execProgressCardWindowState{}
		}
		return execProgressCardWindowState{
			NewCard:  activeExecCommandProgressSegmentMessageID(progress) != "",
			StartSeq: startSeq,
			EndSeq:   startSeq - 1,
			Lines:    append([]execProgressRenderedLine(nil), transient...),
		}
	}
	windowIndex := 0
	for index, line := range persistent {
		if line.Seq >= startSeq {
			windowIndex = index
			break
		}
		if index == len(persistent)-1 {
			windowIndex = 0
		}
	}
	for windowIndex < len(persistent) {
		window, ok := buildExecProgressCardWindow(persistent, transient, windowIndex, subtitle)
		if ok {
			if window.StartSeq > startSeq {
				window.NewCard = true
			}
			return window
		}
		windowIndex++
	}
	lastLine := persistent[len(persistent)-1]
	fallbackLines := append(execProgressCarryoverLines(persistent, len(persistent)-1), lastLine)
	if execProgressCardFits(fallbackLines, subtitle) {
		return execProgressCardWindowState{
			NewCard:  activeExecCommandProgressSegmentMessageID(progress) != "",
			StartSeq: lastLine.Seq,
			EndSeq:   lastLine.Seq,
			Lines:    fallbackLines,
		}
	}
	fallbackLines = []execProgressRenderedLine{lastLine}
	if execProgressCardFits(fallbackLines, subtitle) {
		return execProgressCardWindowState{
			NewCard:  activeExecCommandProgressSegmentMessageID(progress) != "",
			StartSeq: lastLine.Seq,
			EndSeq:   lastLine.Seq,
			Lines:    fallbackLines,
		}
	}
	if truncated, ok := truncateExecProgressRenderedLineToFit(lastLine, subtitle); ok {
		return execProgressCardWindowState{
			NewCard:  activeExecCommandProgressSegmentMessageID(progress) != "",
			StartSeq: lastLine.Seq,
			EndSeq:   lastLine.Seq,
			Lines:    []execProgressRenderedLine{truncated},
		}
	}
	return execProgressCardWindowState{}
}

func truncateExecProgressRenderedLineToFit(line execProgressRenderedLine, subtitle string) (execProgressRenderedLine, bool) {
	const suffix = "..."
	content := strings.TrimSpace(line.Content)
	if content == "" {
		return execProgressRenderedLine{}, false
	}
	runes := []rune(content)
	if len(runes) <= len([]rune(suffix)) {
		return execProgressRenderedLine{}, false
	}
	low, high := 1, len(runes)-len([]rune(suffix))
	var best string
	for low <= high {
		mid := (low + high) / 2
		candidate := strings.TrimSpace(string(runes[:mid])) + suffix
		testLine := line
		testLine.Content = candidate
		if execProgressCardFits([]execProgressRenderedLine{testLine}, subtitle) {
			best = candidate
			low = mid + 1
			continue
		}
		high = mid - 1
	}
	if best == "" {
		return execProgressRenderedLine{}, false
	}
	line.Content = best
	return line, true
}

func buildExecProgressCardWindow(persistent, transient []execProgressRenderedLine, windowIndex int, subtitle string) (execProgressCardWindowState, bool) {
	if windowIndex >= len(persistent) {
		return execProgressCardWindowState{}, false
	}
	base := append(execProgressCarryoverLines(persistent, windowIndex), persistent[windowIndex:]...)
	lines := append([]execProgressRenderedLine(nil), base...)
	if len(transient) != 0 {
		lines = append(lines, transient...)
		if execProgressCardFits(lines, subtitle) {
			return execProgressCardWindowState{
				StartSeq: persistent[windowIndex].Seq,
				EndSeq:   persistent[len(persistent)-1].Seq,
				Lines:    lines,
			}, true
		}
	}
	if !execProgressCardFits(base, subtitle) {
		return execProgressCardWindowState{}, false
	}
	return execProgressCardWindowState{
		StartSeq: persistent[windowIndex].Seq,
		EndSeq:   persistent[len(persistent)-1].Seq,
		Lines:    base,
	}, true
}

func execProgressCarryoverLines(persistent []execProgressRenderedLine, windowIndex int) []execProgressRenderedLine {
	if windowIndex <= 0 || windowIndex > len(persistent) {
		return nil
	}
	lines := make([]execProgressRenderedLine, 0, windowIndex)
	for _, line := range persistent[:windowIndex] {
		if !line.CarryoverEligible {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
