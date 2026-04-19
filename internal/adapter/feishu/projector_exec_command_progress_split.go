package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type execProgressRenderedLine struct {
	Seq       int
	Content   string
	Transient bool
}

type execProgressCardWindowState struct {
	StartSeq int
	EndSeq   int
	Lines    []execProgressRenderedLine
}

const execProgressOmittedHistoryText = "较早过程已省略，仅保留最近进度。"

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
			Seq:     item.LastSeq,
			Content: content,
		})
	}
	return lines
}

func normalizeExecProgressCardStartSeq(progress control.ExecCommandProgress, lines []execProgressRenderedLine) int {
	firstPersistent := 0
	for _, line := range lines {
		if line.Transient || line.Seq <= 0 {
			continue
		}
		if firstPersistent == 0 {
			firstPersistent = line.Seq
		}
		if progress.CardStartSeq > 0 && line.Seq >= progress.CardStartSeq {
			return progress.CardStartSeq
		}
	}
	if firstPersistent > 0 {
		return firstPersistent
	}
	if progress.CardStartSeq > 0 {
		return progress.CardStartSeq
	}
	return 1
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

func execProgressOmittedHistoryLine() execProgressRenderedLine {
	return execProgressRenderedLine{
		Content: formatNeutralTextTag(execProgressOmittedHistoryText),
	}
}

func execProgressCardFits(lines []execProgressRenderedLine) bool {
	if len(lines) == 0 {
		return true
	}
	op := Operation{
		Kind:            OperationSendCard,
		CardTitle:       "工作中",
		CardThemeKey:    cardThemeProgress,
		CardElements:    execProgressRenderedElements(lines),
		CardUpdateMulti: true,
		cardEnvelope:    cardEnvelopeV2,
		card:            rawCardDocument("工作中", "", cardThemeProgress, execProgressRenderedElements(lines)),
	}
	payload := renderOperationCard(op, op.ordinaryCardEnvelope())
	size, err := jsonSize(payload)
	return err == nil && size <= maxFeishuCardBytes
}

func execProgressCardWindow(progress control.ExecCommandProgress, lines []execProgressRenderedLine) execProgressCardWindowState {
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
		if len(transient) == 0 || !execProgressCardFits(transient) {
			return execProgressCardWindowState{}
		}
		return execProgressCardWindowState{
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
		window, ok := buildExecProgressCardWindow(persistent, transient, windowIndex)
		if ok {
			return window
		}
		windowIndex++
	}
	lastLine := persistent[len(persistent)-1]
	fallbackLines := []execProgressRenderedLine{lastLine}
	if execProgressCardFits(fallbackLines) {
		return execProgressCardWindowState{
			StartSeq: lastLine.Seq,
			EndSeq:   lastLine.Seq,
			Lines:    fallbackLines,
		}
	}
	return execProgressCardWindowState{}
}

func buildExecProgressCardWindow(persistent, transient []execProgressRenderedLine, windowIndex int) (execProgressCardWindowState, bool) {
	if windowIndex >= len(persistent) {
		return execProgressCardWindowState{}, false
	}
	base := append([]execProgressRenderedLine(nil), persistent[windowIndex:]...)
	if windowIndex > 0 {
		base = append([]execProgressRenderedLine{execProgressOmittedHistoryLine()}, base...)
	}
	lines := append([]execProgressRenderedLine(nil), base...)
	if len(transient) != 0 {
		lines = append(lines, transient...)
		if execProgressCardFits(lines) {
			return execProgressCardWindowState{
				StartSeq: persistent[windowIndex].Seq,
				EndSeq:   persistent[len(persistent)-1].Seq,
				Lines:    lines,
			}, true
		}
	}
	if !execProgressCardFits(base) {
		return execProgressCardWindowState{}, false
	}
	return execProgressCardWindowState{
		StartSeq: persistent[windowIndex].Seq,
		EndSeq:   persistent[len(persistent)-1].Seq,
		Lines:    base,
	}, true
}
