package shutdownctx

import (
	"context"
	"sync/atomic"
)

type Mode string

const (
	ModeDefault      Mode = ""
	ModeConsoleClose Mode = "console_close"
)

type holder struct {
	mode atomic.Int32
}

type contextKey struct{}

func WithHolder(parent context.Context) (context.Context, func(Mode)) {
	h := &holder{}
	ctx := context.WithValue(parent, contextKey{}, h)
	return ctx, func(mode Mode) {
		h.mode.Store(int32(modeToIndex(mode)))
	}
}

func ModeFrom(ctx context.Context) Mode {
	if ctx == nil {
		return ModeDefault
	}
	h, _ := ctx.Value(contextKey{}).(*holder)
	if h == nil {
		return ModeDefault
	}
	return modeFromIndex(int(h.mode.Load()))
}

func modeToIndex(mode Mode) int {
	switch mode {
	case ModeConsoleClose:
		return 1
	default:
		return 0
	}
}

func modeFromIndex(value int) Mode {
	switch value {
	case 1:
		return ModeConsoleClose
	default:
		return ModeDefault
	}
}
