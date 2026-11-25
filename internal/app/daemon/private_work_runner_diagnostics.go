package daemon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

const (
	privateRunnerDiagnosticHistory = 128
	privateRunnerDiagnosticBudget  = 16
)

type privateRunnerFailure struct {
	stage, effectID string
	reason          [sha256.Size]byte
}

type privateRunnerDiagnostics struct {
	mu       sync.Mutex
	recent   []privateRunnerFailure
	window   time.Time
	reported int
}

func (d *privateRunnerDiagnostics) report(stage, effectID string, err error) {
	if message := d.message(time.Now(), stage, effectID, err); message != "" {
		log.Print(message)
	}
}

func (d *privateRunnerDiagnostics) message(now time.Time, stage, effectID string, err error) string {
	if err != nil && !privateRunnerUnexpectedError(err) {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	index := slices.IndexFunc(d.recent, func(failure privateRunnerFailure) bool {
		return failure.stage == stage && failure.effectID == effectID
	})
	if err == nil {
		if index >= 0 {
			d.recent = slices.Delete(d.recent, index, index+1)
		}
		return ""
	}
	reason := err.Error()
	failure := privateRunnerFailure{stage: stage, effectID: effectID, reason: sha256.Sum256([]byte(reason))}
	if index >= 0 && d.recent[index].reason == failure.reason {
		return ""
	}
	if d.window.IsZero() || now.Sub(d.window) >= time.Minute {
		d.window = now
		d.reported = 0
	}
	if d.reported >= privateRunnerDiagnosticBudget {
		return ""
	}
	d.reported++
	if index >= 0 {
		d.recent = slices.Delete(d.recent, index, index+1)
	} else if len(d.recent) == privateRunnerDiagnosticHistory {
		d.recent = slices.Delete(d.recent, 0, 1)
	}
	d.recent = append(d.recent, failure)
	return fmt.Sprintf("private work runner stage=%s effect=%q error=%q", stage, effectID, reason)
}

func privateRunnerUnexpectedError(err error) bool {
	switch err {
	case nil, context.Canceled, privatechat.ErrDeferred, privatechat.ErrDisplayHeld, errPrivatePublicationUnknown:
		return false
	}
	// Publication failures join the uncertain delivery marker with the original
	// transport or journal error. Only a tree of expected markers is quiet.
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		return slices.ContainsFunc(wrapped.Unwrap(), privateRunnerUnexpectedError)
	case interface{ Unwrap() error }:
		return privateRunnerUnexpectedError(wrapped.Unwrap())
	default:
		return true
	}
}
