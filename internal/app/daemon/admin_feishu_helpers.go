package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func resetFeishuVerification(app *config.FeishuAppConfig) {
	if app == nil {
		return
	}
	app.VerifiedAt = nil
}

func decodeJSONBody(r *http.Request, target any) error {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}

func indexOfConfigFeishuApp(apps []config.FeishuAppConfig, gatewayID string) int {
	for i, app := range apps {
		if canonicalGatewayID(app.ID) == canonicalGatewayID(gatewayID) {
			return i
		}
	}
	return -1
}

func nextGatewayID(apps []config.FeishuAppConfig, admin adminRuntimeState, req feishuAppWriteRequest) string {
	base := sanitizeGatewayPath(firstNonEmpty(trimmedString(req.Name), trimmedString(req.AppID), "app"))
	if base == "" {
		base = "app"
	}
	if canonicalGatewayID(base) == canonicalGatewayID(admin.envOverrideGatewayID) && admin.envOverrideActive {
		base += "-config"
	}
	exists := func(candidate string) bool {
		if indexOfConfigFeishuApp(apps, candidate) >= 0 {
			return true
		}
		if admin.envOverrideActive && canonicalGatewayID(candidate) == canonicalGatewayID(admin.envOverrideGatewayID) {
			return true
		}
		return false
	}
	if !exists(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !exists(candidate) {
			return candidate
		}
	}
}

func trimmedString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func canonicalGatewayID(gatewayID string) string {
	return strings.TrimSpace(gatewayID)
}

func daemonBoolPtr(value bool) *bool {
	return &value
}

func buildFeishuAppMutation(appIDChanged, secretChanged bool) *feishuAppMutationView {
	switch {
	case appIDChanged:
		return &feishuAppMutationView{
			Kind:               "identity_changed",
			Message:            "已切换到另一个飞书 App。旧会话不会自动迁移，请在飞书里打开新机器人的会话重新开始；测试连接只验证新凭证。",
			ReconnectRequested: true,
			RequiresNewChat:    true,
		}
	case secretChanged:
		return &feishuAppMutationView{
			Kind:               "credentials_changed",
			Message:            "飞书凭证已更新，运行时已请求重新连接。请确认连接状态恢复后再继续使用。",
			ReconnectRequested: true,
		}
	default:
		return &feishuAppMutationView{
			Kind:    "updated",
			Message: "飞书机器人配置已更新。",
		}
	}
}

func buildCreatedFeishuAppMutation() *feishuAppMutationView {
	return &feishuAppMutationView{
		Kind:    "created",
		Message: "飞书机器人已创建。接下来请先测试连接，并完成首次配置。",
	}
}

func errFeishuAppNotFound(gatewayID string) error {
	return fmt.Errorf("feishu_app_not_found:%s", strings.TrimSpace(gatewayID))
}

func errFeishuAppRuntimeUnavailable(gatewayID string) error {
	return fmt.Errorf("feishu_app_runtime_unavailable:%s", strings.TrimSpace(gatewayID))
}

func buildFeishuAppConsoleLinks(appID string) feishuAppConsoleLinks {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return feishuAppConsoleLinks{}
	}
	base := "https://open.feishu.cn/app/" + appID
	return feishuAppConsoleLinks{
		Auth:     base + "/auth",
		Events:   base + "/event?tab=event",
		Callback: base + "/event?tab=callback",
		Bot:      base + "/bot",
	}
}
