package codexupgraderuntime

import (
	"context"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
)

type Transaction struct {
	ID                 string
	Install            codexupgrade.Installation
	CurrentVersion     string
	TargetVersion      string
	InitiatorSurface   string
	InitiatorUserID    string
	RestartInstanceIDs []string
	PausedSurfaceIDs   map[string]bool
	StartedAt          time.Time
}

type State struct {
	LatestLookup func(context.Context) (string, error)
	Install      func(context.Context, codexupgrade.Installation, string) error
	Active       *Transaction
	NextSeq      int64
}

func NewState() State {
	return State{}
}
