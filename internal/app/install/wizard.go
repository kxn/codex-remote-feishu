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
		opts.RelayServerURL = relayServerURLForPort(instanceDefaultPorts(opts.InstanceID).Relay)
	}

	fmt.Fprintln(out, "Codex Remote Feishu 安装向导")
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "当前平台: %s\n", defaults.GOOS)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "这一步会完成：")
	fmt.Fprintln(out, "- 安装 codex-remote 统一二进制到稳定路径")
	fmt.Fprintln(out, "- 写入统一配置文件 config.json")
	fmt.Fprintln(out, "")

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
	fmt.Fprintln(out, "- 先为这套机器人配置一个明确的 gateway id，例如 main。")
	fmt.Fprintln(out, "- 在“凭证与基础信息”页面复制 App ID / App Secret。")
	fmt.Fprintln(out, "- 在“事件与回调”里订阅消息、reaction、菜单事件。")
	fmt.Fprintln(out, "- 工程内的 deploy/feishu 目录提供默认权限清单模板。")
	feishuGatewayID, err := promptString(reader, out, "Feishu Gateway ID", opts.FeishuGatewayID)
	if err != nil {
		return Options{}, err
	}
	opts.FeishuGatewayID = feishuGatewayID
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
	fmt.Fprintf(out, "- 如果 relayd 跑在本机，保持默认 %s 即可。\n", relayServerURLForPort(instanceDefaultPorts(opts.InstanceID).Relay))
	fmt.Fprintln(out, "- 只有在 relayd 跑在其他机器时，才需要改成对应地址。")
	relayURL, err := promptString(reader, out, "Relay WebSocket 地址", opts.RelayServerURL)
	if err != nil {
		return Options{}, err
	}
	opts.RelayServerURL = relayURL

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Codex 路径覆盖（可选）：")
	fmt.Fprintln(out, "- 只有你需要手动指定 Codex 路径时，才需要填写。")
	fmt.Fprintln(out, "- 留空会保留已有配置，首次安装使用默认路径。")
	defaultCodexRealBinary := opts.CodexRealBinary
	codexRealBinary, err := promptString(reader, out, "Codex 可执行文件路径（可选）", defaultCodexRealBinary)
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
