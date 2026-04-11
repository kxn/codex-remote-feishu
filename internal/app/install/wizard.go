package install

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func RunInteractiveWizard(in io.Reader, out io.Writer, defaults PlatformDefaults, seed Options) (Options, error) {
	reader := bufio.NewReader(in)
	opts := seed
	if strings.TrimSpace(opts.BaseDir) == "" {
		opts.BaseDir = defaults.BaseDir
	}
	if strings.TrimSpace(opts.InstallBinDir) == "" {
		opts.InstallBinDir = defaults.InstallBinDir
	}
	if strings.TrimSpace(opts.RelayServerURL) == "" {
		opts.RelayServerURL = "ws://127.0.0.1:9500/ws/agent"
	}
	if strings.TrimSpace(opts.VSCodeSettingsPath) == "" {
		opts.VSCodeSettingsPath = defaults.VSCodeSettingsPath
	}
	if len(opts.Integrations) == 0 {
		opts.Integrations = defaults.DefaultIntegrations
	}
	if strings.TrimSpace(opts.BundleEntrypoint) == "" {
		opts.BundleEntrypoint = recommendedBundleEntrypoint(defaults)
	}

	fmt.Fprintln(out, "Codex Remote Feishu 安装向导")
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "当前平台: %s\n", defaults.GOOS)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "这一步会完成：")
	fmt.Fprintln(out, "- 安装 codex-remote 统一二进制到稳定路径")
	fmt.Fprintln(out, "- 写入统一配置文件 config.json")
	fmt.Fprintln(out, "- 按你的选择接管 VS Code")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, integrationHelpText(defaults.GOOS))

	selection, err := promptString(reader, out,
		"请选择集成方式 [1=managed_shim；兼容输入 2/3 也会收敛为 managed_shim]",
		integrationPromptDefault(opts.Integrations, defaults.GOOS),
	)
	if err != nil {
		return Options{}, err
	}
	switch strings.TrimSpace(selection) {
	case "", "1", "2", "3":
		opts.Integrations = []WrapperIntegrationMode{IntegrationManagedShim}
	default:
		return Options{}, fmt.Errorf("unsupported integration selection: %s", selection)
	}

	baseDir, err := promptString(reader, out, "安装根目录", opts.BaseDir)
	if err != nil {
		return Options{}, err
	}
	opts.BaseDir = baseDir

	installBinDir, err := promptString(reader, out, "安装二进制目录", opts.InstallBinDir)
	if err != nil {
		return Options{}, err
	}
	opts.InstallBinDir = installBinDir

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Feishu 配置说明：")
	fmt.Fprintln(out, "- 打开飞书开放平台，进入你的自建应用。")
	fmt.Fprintln(out, "- 在“凭证与基础信息”页面复制 App ID / App Secret。")
	fmt.Fprintln(out, "- 在“事件与回调”里订阅消息、reaction、菜单事件。")
	fmt.Fprintln(out, "- 如果你暂时不确定缺什么，先完成安装，再按 WebSetup 里的能力检查继续补齐。")
	feishuAppID, err := promptString(reader, out, "Feishu App ID", opts.FeishuAppID)
	if err != nil {
		return Options{}, err
	}
	opts.FeishuAppID = feishuAppID
	feishuSecret, err := promptString(reader, out, "Feishu App Secret", opts.FeishuAppSecret)
	if err != nil {
		return Options{}, err
	}
	opts.FeishuAppSecret = feishuSecret

	useProxy, err := promptBool(reader, out, "relayd 访问飞书 API 时是否使用系统代理", opts.UseSystemProxy)
	if err != nil {
		return Options{}, err
	}
	opts.UseSystemProxy = useProxy

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Relay 配置说明：")
	fmt.Fprintln(out, "- 如果 relayd 跑在本机，保持默认 ws://127.0.0.1:9500/ws/agent 即可。")
	fmt.Fprintln(out, "- 如果 relayd 跑在 Docker 并映射到本机端口，同样可保持默认值。")
	fmt.Fprintln(out, "- 只有在 relayd 跑在其他机器时，才需要改成对应地址。")
	relayURL, err := promptString(reader, out, "Relay WebSocket 地址", opts.RelayServerURL)
	if err != nil {
		return Options{}, err
	}
	opts.RelayServerURL = relayURL

	if hasIntegration(opts.Integrations, IntegrationManagedShim) {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "managed_shim 说明：")
		fmt.Fprintln(out, "- 当前唯一推荐的 VS Code 接入方式。")
		fmt.Fprintln(out, "- 安装器会直接替换扩展 bundle 里的 codex 入口，并保留原始 codex.real。")
		fmt.Fprintln(out, "- 不会修改客户端侧 settings.json，因此不会把 host 机器上的 override 带进 Remote SSH 会话。")
		fmt.Fprintln(out, "- 请先关闭 VS Code / VS Code Remote，避免 Windows 或 macOS 上文件被占用。")
		if len(defaults.CandidateBundleEntrypoints) > 0 {
			fmt.Fprintln(out, "检测到这些候选 bundle 入口：")
			for i, candidate := range defaults.CandidateBundleEntrypoints {
				fmt.Fprintf(out, "  %d. %s\n", i+1, candidate)
			}
		} else {
			fmt.Fprintln(out, "当前没有自动探测到 bundle 入口，需要你手动填写。")
		}
		bundleEntrypoint, err := promptString(reader, out, "扩展 bundle codex 入口路径", opts.BundleEntrypoint)
		if err != nil {
			return Options{}, err
		}
		opts.BundleEntrypoint = bundleEntrypoint
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "真实 Codex 配置说明：")
	fmt.Fprintln(out, "- 当前默认的 managed_shim 模式下，如未显式填写，会自动使用 bundle 里保留下来的 codex.real。")
	fmt.Fprintln(out, "- 只有兼容旧配置或你明确要覆盖默认行为时，才需要手动填写真实 codex 路径。")
	defaultCodexRealBinary := opts.CodexRealBinary
	if strings.TrimSpace(defaultCodexRealBinary) == "" && !hasIntegration(opts.Integrations, IntegrationManagedShim) {
		defaultCodexRealBinary = "codex"
	}
	codexRealBinary, err := promptString(reader, out, "真实 Codex 可执行文件路径", defaultCodexRealBinary)
	if err != nil {
		return Options{}, err
	}
	opts.CodexRealBinary = codexRealBinary

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "安装输入已确认。")
	return opts, nil
}

func promptString(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	if strings.TrimSpace(defaultValue) != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}

func promptBool(reader *bufio.Reader, out io.Writer, label string, defaultValue bool) (bool, error) {
	defaultPrompt := "n"
	if defaultValue {
		defaultPrompt = "y"
	}
	value, err := promptString(reader, out, label+" [y/n]", defaultPrompt)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "true", "1":
		return true, nil
	case "n", "no", "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("unsupported boolean selection: %s", value)
	}
}

func integrationPromptDefault(values []WrapperIntegrationMode, goos string) string {
	if len(values) == 0 {
		values = DefaultIntegrations(goos)
	}
	return "1"
}
