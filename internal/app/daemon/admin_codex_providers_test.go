package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestAdminCodexProvidersCompatibilityProjectionAndRedaction(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":"Team Proxy",
  "baseURL":"https://proxy.internal/v1",
  "apiKey":"secret-key",
  "model":"gpt-5.4",
  "reasoningEffort":"high"
}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	if strings.Contains(create.Body.String(), "secret-key") {
		t.Fatalf("legacy response leaked API key: %s", create.Body.String())
	}
	var created codexProviderResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !strings.HasPrefix(created.Provider.ID, "cp_") || !created.Provider.HasAPIKey || created.Provider.Model != "gpt-5.4" {
		t.Fatalf("unexpected legacy projection: %#v", created.Provider)
	}

	list := performAdminRequest(t, app, http.MethodGet, "/api/admin/codex/providers", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var listed codexProvidersResponse
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Providers) != 2 || listed.Providers[0].ID != config.CodexDefaultProviderID || listed.Providers[1].ID != created.Provider.ID {
		t.Fatalf("unexpected legacy list: %#v", listed.Providers)
	}
	if !listed.Providers[0].BuiltIn || !listed.Providers[0].ReadOnly || listed.Providers[0].Persisted {
		t.Fatalf("unexpected native compatibility view: %#v", listed.Providers[0])
	}

	adminConfig := performAdminRequest(t, app, http.MethodGet, "/api/admin/config", "")
	if adminConfig.Code != http.StatusOK || strings.Contains(adminConfig.Body.String(), "secret-key") {
		t.Fatalf("admin config redaction failed: status=%d body=%s", adminConfig.Code, adminConfig.Body.String())
	}
	var configResponse adminConfigResponse
	if err := json.NewDecoder(adminConfig.Body).Decode(&configResponse); err != nil {
		t.Fatalf("decode admin config: %v", err)
	}
	if len(configResponse.Config.Codex.Providers) != 1 || !configResponse.Config.Codex.Providers[0].HasAPIKey {
		t.Fatalf("unexpected admin config compatibility view: %#v", configResponse.Config.Codex)
	}
}

func TestAdminCodexProviderCompatibilityUsesCanonicalValidation(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	for _, body := range []string{
		`{"name":" ","baseURL":"https://proxy.internal/v1","apiKey":"secret","model":"gpt-5.4","reasoningEffort":"high"}`,
		`{"name":"Missing Key","baseURL":"https://proxy.internal/v1","model":"gpt-5.4","reasoningEffort":"high"}`,
		`{"name":"Missing Model","baseURL":"https://proxy.internal/v1","apiKey":"secret","reasoningEffort":"high"}`,
		`{"name":"Missing Reasoning","baseURL":"https://proxy.internal/v1","apiKey":"secret","model":"gpt-5.4"}`,
		`{"name":"Bad Reasoning","baseURL":"https://proxy.internal/v1","apiKey":"secret","model":"gpt-5.4","reasoningEffort":"bad\nvalue"}`,
	} {
		rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid compatibility create status = %d body=%s", rec.Code, rec.Body.String())
		}
	}

	customReasoning := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/providers", `{
  "name":"中文代理",
  "baseURL":"https://proxy.internal/v1",
  "apiKey":"secret-key",
  "model":"gpt-5.4",
  "reasoningEffort":"turbo"
}`)
	if customReasoning.Code != http.StatusCreated {
		t.Fatalf("open reasoning create status = %d body=%s", customReasoning.Code, customReasoning.Body.String())
	}
	var response codexProviderResponse
	if err := json.NewDecoder(customReasoning.Body).Decode(&response); err != nil {
		t.Fatalf("decode custom reasoning create: %v", err)
	}
	if !strings.HasPrefix(response.Provider.ID, "cp_") || response.Provider.Name != "中文代理" || response.Provider.ReasoningEffort != "turbo" {
		t.Fatalf("unexpected open reasoning profile: %#v", response.Provider)
	}
}
