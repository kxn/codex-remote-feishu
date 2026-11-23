package daemon

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	"strings"
	"time"
)

// probePrivateOpenCodeVersion runs the selected binary before durable spawn so
// an unsupported version can be reported to the user. The wrapper independently
// rechecks the spawned ACP process's initialize identity before accepting work.
func (a *App) probePrivateOpenCodeVersion(ctx context.Context, env []string, cwd string) error {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return err
	}
	binary, err := config.ResolveOpenCodeBinary(env, loaded.Config.OpenCode.BinaryPath)
	if err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := execlaunch.CommandContext(probeCtx, binary, "--version")
	cmd.Env = env
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("无法检测 OpenCode 版本：%w", err)
	}
	version := strings.TrimSpace(string(out))
	if !agentproto.SupportedPrivateOpenCodeVersion(version) {
		return agentproto.ErrorInfo{Code: "opencode_upgrade_required", Layer: "daemon", Stage: "headless_start", Operation: "start_headless", Message: "当前 OpenCode 版本不支持单聊，请升级到 OpenCode 2.1.4 或更新的 2.x 版本。", Details: "当前版本：" + version}
	}
	return nil
}
