package daemon

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestAdminVisionAssistReadWrite(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	initial := performAdminRequest(t, app, http.MethodGet, "/api/admin/vision-assist", "")
	if initial.Code != http.StatusOK {
		t.Fatalf("initial status = %d body=%s", initial.Code, initial.Body.String())
	}
	var initialResponse visionAssistResponse
	if err := json.NewDecoder(initial.Body).Decode(&initialResponse); err != nil {
		t.Fatalf("decode initial: %v", err)
	}
	if initialResponse.Configured {
		t.Fatalf("expected unconfigured vision assist, got %#v", initialResponse)
	}

	put := performAdminRequest(t, app, http.MethodPut, "/api/admin/vision-assist", `{
		"baseURL": "https://api.example.com/v1",
		"apiKey": "secret-key",
		"model": "gpt-v"
	}`)
	if put.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", put.Code, put.Body.String())
	}

	loaded := performAdminRequest(t, app, http.MethodGet, "/api/admin/vision-assist", "")
	var loadedResponse visionAssistResponse
	if err := json.NewDecoder(loaded.Body).Decode(&loadedResponse); err != nil {
		t.Fatalf("decode loaded: %v", err)
	}
	if !loadedResponse.Configured {
		t.Fatalf("expected configured vision assist, got %#v", loadedResponse)
	}
	if loadedResponse.Settings.Protocol != "openai_chat" ||
		loadedResponse.Settings.BaseURL != "https://api.example.com/v1" ||
		loadedResponse.Settings.Model != "gpt-v" {
		t.Fatalf("unexpected vision assist settings %#v", loadedResponse.Settings)
	}
	if !loadedResponse.HasAPIKey {
		t.Fatalf("expected hasAPIKey true after saving api key, got %#v", loadedResponse)
	}
	if loadedResponse.Settings.APIKey != "" {
		t.Fatalf("api key must not be echoed back, got %#v", loadedResponse.Settings)
	}

	// PUT 未提供 apiKey 时保留现有明文 key。
	put = performAdminRequest(t, app, http.MethodPut, "/api/admin/vision-assist", `{
		"baseURL": "https://api.example.com/v1",
		"model": "gpt-v2"
	}`)
	if put.Code != http.StatusOK {
		t.Fatalf("put without api key status = %d body=%s", put.Code, put.Body.String())
	}
	loaded = performAdminRequest(t, app, http.MethodGet, "/api/admin/vision-assist", "")
	if err := json.NewDecoder(loaded.Body).Decode(&loadedResponse); err != nil {
		t.Fatalf("decode reloaded: %v", err)
	}
	if !loadedResponse.HasAPIKey || loadedResponse.Settings.Model != "gpt-v2" {
		t.Fatalf("expected api key preserved and model updated, got %#v", loadedResponse)
	}
}

func TestAdminVisionAssistRejectsInvalidProtocol(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	put := performAdminRequest(t, app, http.MethodPut, "/api/admin/vision-assist", `{
		"protocol": "unknown",
		"baseURL": "https://api.example.com/v1"
	}`)
	if put.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for unknown protocol, got %d body=%s", put.Code, put.Body.String())
	}

	put = performAdminRequest(t, app, http.MethodPut, "/api/admin/vision-assist", `{
		"protocol": "openai_chat"
	}`)
	if put.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for missing base url, got %d body=%s", put.Code, put.Body.String())
	}
}
