//go:build !windows

package launcher

import "context"

func registerPlatformConsoleCloseBridge(context.CancelFunc) (func(), error) {
	return nil, nil
}
