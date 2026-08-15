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
		"protocol": "openai_chat",
		"baseURL": "https://api.example.com/v1",
		"apiKeyEnv": "VISION_API_KEY",
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
		loadedResponse.Settings.APIKeyEnv != "VISION_API_KEY" ||
		loadedResponse.Settings.Model != "gpt-v" {
		t.Fatalf("unexpected vision assist settings %#v", loadedResponse.Settings)
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
