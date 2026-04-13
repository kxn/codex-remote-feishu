//go:build !windows

package launcher

import "context"

func registerPlatformConsoleCloseBridge(func()) (func(), error) {
	return nil, nil
}
