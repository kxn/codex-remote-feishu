package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

func markFeishuCredentialsSaved(app *config.FeishuAppConfig, at time.Time) {
	if app == nil {
		return
	}
	if strings.TrimSpace(app.AppID) == "" || strings.TrimSpace(app.AppSecret) == "" {
		return
	}
	value := at.UTC()
	app.Wizard.CredentialsSavedAt = &value
}

func resetFeishuVerification(app *config.FeishuAppConfig) {
	if app == nil {
		return
	}
	app.VerifiedAt = nil
	app.Wizard.ConnectionVerifiedAt = nil
}

func resetFeishuWizardManualSteps(app *config.FeishuAppConfig) {
	if app == nil {
		return
	}
	app.Wizard.ScopesExportedAt = nil
	app.Wizard.EventsConfirmedAt = nil
	app.Wizard.CallbacksConfirmedAt = nil
	app.Wizard.MenusConfirmedAt = nil
	app.Wizard.PublishedAt = nil
}

func applyWizardToggle(target **time.Time, enabled *bool, at time.Time) {
	if enabled == nil || target == nil {
		return
	}
	if *enabled {
		value := at.UTC()
		*target = &value
		return
	}
	*target = nil
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
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return feishu.LegacyDefaultGatewayID
	}
	return gatewayID
}

func daemonBoolPtr(value bool) *bool {
	return &value
}
