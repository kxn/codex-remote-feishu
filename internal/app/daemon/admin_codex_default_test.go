package daemon

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestAdminCodexRemoteDefaultReadWriteAndClear(t *testing.T) {
	app, path := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	put := performAdminRequest(t, app, http.MethodPut, "/api/admin/codex/default-model", `{"model":"gpt-6-sol","reasoningEffort":"high"}`)
	if put.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", put.Code, put.Body.String())
	}
	loaded, err := config.LoadAppConfigAtPath(path)
	if err != nil || loaded.Config.Codex.DefaultModel != "gpt-6-sol" || loaded.Config.Codex.DefaultReasoningEffort != "high" {
		t.Fatalf("persisted default = %#v, err=%v", loaded.Config.Codex, err)
	}
	get := performAdminRequest(t, app, http.MethodGet, "/api/admin/codex/default-model", "")
	var response codexRemoteDefaultResponse
	if err := json.NewDecoder(get.Body).Decode(&response); err != nil || response.Model != "gpt-6-sol" || response.ReasoningEffort != "high" {
		t.Fatalf("readback = %#v, err=%v", response, err)
	}

	bad := performAdminRequest(t, app, http.MethodPut, "/api/admin/codex/default-model", `{"model":"gpt-6-sol","reasoningEffort":"invalid"}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid effort status = %d", bad.Code)
	}
	clear := performAdminRequest(t, app, http.MethodPut, "/api/admin/codex/default-model", `{"model":"","reasoningEffort":""}`)
	if clear.Code != http.StatusOK {
		t.Fatalf("clear status = %d body=%s", clear.Code, clear.Body.String())
	}
}
