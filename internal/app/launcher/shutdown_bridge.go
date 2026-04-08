package launcher

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

var registerPlatformShutdownBridge = registerPlatformConsoleCloseBridge

func newMainContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	if parent == nil {
		parent = context.Background()
	}

	ctx, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	unregister, err := registerPlatformShutdownBridge(stopSignals)
	if err != nil {
		stopSignals()
		return nil, nil, err
	}

	stop := func() {
		if unregister != nil {
			unregister()
		}
		stopSignals()
	}
	return ctx, stop, nil
}
