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

type execProgressCardChunk struct {
	StartSeq int
	EndSeq   int
	Lines    []execProgressRenderedLine
}

func execCommandProgressRenderedLines(progress control.ExecCommandProgress) []execProgressRenderedLine {
	items := normalizedExecProgressTimeline(progress)
	lines := make([]execProgressRenderedLine, 0, len(items))
	for _, item := range items {
		content := renderExecProgressTimelineItem(item)
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

func partitionExecProgressChunks(lines []execProgressRenderedLine, startSeq int) []execProgressCardChunk {
	persistent := make([]execProgressRenderedLine, 0, len(lines))
	transient := make([]execProgressRenderedLine, 0, 1)
	for _, line := range lines {
		if line.Transient {
			transient = append(transient, line)
			continue
		}
		if line.Seq < startSeq {
			continue
		}
		persistent = append(persistent, line)
	}
	if len(persistent) == 0 {
		if len(transient) == 0 || !execProgressCardFits(transient) {
			return nil
		}
		return []execProgressCardChunk{{
			StartSeq: startSeq,
			EndSeq:   startSeq - 1,
			Lines:    append([]execProgressRenderedLine(nil), transient...),
		}}
	}

	chunks := make([]execProgressCardChunk, 0, 4)
	for index := 0; index < len(persistent); {
		current := make([]execProgressRenderedLine, 0, 8)
		next := index
		for next < len(persistent) {
			candidate := append(append([]execProgressRenderedLine(nil), current...), persistent[next])
			if !execProgressCardFits(candidate) {
				break
			}
			current = candidate
			next++
		}
		if len(current) == 0 {
			current = append(current, persistent[index])
			next = index + 1
		}
		chunks = append(chunks, execProgressCardChunk{
			StartSeq: current[0].Seq,
			EndSeq:   current[len(current)-1].Seq,
			Lines:    current,
		})
		index = next
	}
	if len(chunks) == 0 || len(transient) == 0 {
		return chunks
	}
	last := &chunks[len(chunks)-1]
	candidate := append(append([]execProgressRenderedLine(nil), last.Lines...), transient...)
	if execProgressCardFits(candidate) {
		last.Lines = candidate
	}
	return chunks
}
