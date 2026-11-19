package orchestrator

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const (
	asyncUserInputDraftTTL   = time.Hour
	asyncUserInputSeenTTL    = 24 * time.Hour
	asyncUserInputMaxDrafts  = 32
	asyncUserInputMaxRecords = 128
)

func asyncUserInputDisplayText(text string, limit int) string {
	return xutil.TruncateRunes(text, limit, xutil.TruncateOptions{Ellipsis: "…（内容过长，已截断）", ReserveEllipsis: true})
}

// Keep only a small replay tombstone after submission or expiration. In
// particular, completed questions must not retain arbitrary answer/context text.
func retireAsyncUserInput(record *asyncUserInputRecord) {
	record.Submitted = true
	record.Context = ""
	record.Questions = nil
	record.Answers = nil
}

func pruneAsyncUserInputs(runtime *surfaceUIRuntimeRecord, now time.Time) {
	if runtime == nil {
		return
	}
	drafts := 0
	for id, record := range runtime.AsyncUserInputs {
		if record == nil || !now.Before(record.CreatedAt.Add(asyncUserInputSeenTTL)) {
			delete(runtime.AsyncUserInputs, id)
			continue
		}
		if !record.Submitted && !now.Before(record.CreatedAt.Add(asyncUserInputDraftTTL)) {
			retireAsyncUserInput(record)
		}
		if !record.Submitted {
			drafts++
		}
	}
	for drafts > asyncUserInputMaxDrafts {
		oldest := oldestAsyncUserInput(runtime, false)
		retireAsyncUserInput(runtime.AsyncUserInputs[oldest])
		drafts--
	}
	for len(runtime.AsyncUserInputs) > asyncUserInputMaxRecords {
		// Evict compact replay records before any still-answerable question.
		delete(runtime.AsyncUserInputs, oldestAsyncUserInput(runtime, true))
	}
}

func oldestAsyncUserInput(runtime *surfaceUIRuntimeRecord, submitted bool) string {
	id := ""
	var sequence uint64
	for key, record := range runtime.AsyncUserInputs {
		if record.Submitted != submitted {
			continue
		}
		if id == "" || record.Sequence < sequence {
			id, sequence = key, record.Sequence
		}
	}
	return id
}
