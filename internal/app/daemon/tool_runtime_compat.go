package daemon

import (
	toolruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/toolruntime"
)

type ToolRuntimeConfig = toolruntime.Config
type toolServiceInfo = toolruntime.ServiceInfo
type toolRuntimeState = toolruntime.State

func (a *App) SetToolRuntime(cfg ToolRuntimeConfig) {
	a.toolRuntime.Configure(cfg, a.newToolRuntimeHandler())
}

func (a *App) bindToolListenerLocked() error {
	return a.toolRuntime.BindLocked()
}

func (a *App) removeToolServiceStateLocked() {
	a.toolRuntime.RemoveStateLocked()
}
