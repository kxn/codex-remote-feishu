package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type LongConnectionStatus struct {
	OnlineInstanceCount int       `json:"onlineInstanceCount"`
	CheckedAt           time.Time `json:"checkedAt"`
}

type BotInfo struct {
	AppName string
	OpenID  string
}

type tenantAccessTokenHTTPResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
}

type longConnectionStatusHTTPResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		OnlineInstanceCount int `json:"online_instance_cnt"`
	} `json:"data"`
}

type botInfoHTTPResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Bot  struct {
		AppName string `json:"app_name"`
		OpenID  string `json:"open_id"`
	} `json:"bot"`
}

func GetLongConnectionStatus(ctx context.Context, cfg LiveGatewayConfig) (LongConnectionStatus, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	broker := NewFeishuCallBrokerWithHTTPClient(cfg.GatewayID, nil, client)
	token, err := getTenantAccessTokenHTTP(ctx, broker, client, cfg)
	if err != nil {
		return LongConnectionStatus{}, err
	}
	resp, err := DoHTTP(ctx, broker, CallSpec{
		GatewayID:  cfg.GatewayID,
		API:        "event.v1.connection.get",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, httpClient *http.Client) (longConnectionStatusHTTPResponse, error) {
		req, err := http.NewRequestWithContext(callCtx, http.MethodGet, strings.TrimRight(feishuHTTPDomain(cfg), "/")+"/open-apis/event/v1/connection", nil)
		if err != nil {
			return longConnectionStatusHTTPResponse{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		httpResp, err := httpClient.Do(req)
		if err != nil {
			return longConnectionStatusHTTPResponse{}, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return longConnectionStatusHTTPResponse{}, fmt.Errorf("long connection status request failed: status=%d", httpResp.StatusCode)
		}
		var decoded longConnectionStatusHTTPResponse
		if err := json.NewDecoder(io.LimitReader(httpResp.Body, 1<<20)).Decode(&decoded); err != nil {
			return longConnectionStatusHTTPResponse{}, err
		}
		if decoded.Code != 0 {
			return longConnectionStatusHTTPResponse{}, fmt.Errorf("event.v1.connection.get failed: code=%d msg=%s", decoded.Code, decoded.Msg)
		}
		return decoded, nil
	})
	if err != nil {
		return LongConnectionStatus{}, err
	}
	return LongConnectionStatus{
		OnlineInstanceCount: resp.Data.OnlineInstanceCount,
		CheckedAt:           time.Now().UTC(),
	}, nil
}

func GetBotInfo(ctx context.Context, cfg LiveGatewayConfig) (BotInfo, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	broker := NewFeishuCallBrokerWithHTTPClient(cfg.GatewayID, nil, client)
	token, err := getTenantAccessTokenHTTP(ctx, broker, client, cfg)
	if err != nil {
		return BotInfo{}, err
	}
	resp, err := DoHTTP(ctx, broker, CallSpec{
		GatewayID:  cfg.GatewayID,
		API:        "bot.v3.info",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, httpClient *http.Client) (botInfoHTTPResponse, error) {
		req, err := http.NewRequestWithContext(callCtx, http.MethodGet, strings.TrimRight(feishuHTTPDomain(cfg), "/")+"/open-apis/bot/v3/info", nil)
		if err != nil {
			return botInfoHTTPResponse{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		httpResp, err := httpClient.Do(req)
		if err != nil {
			return botInfoHTTPResponse{}, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return botInfoHTTPResponse{}, fmt.Errorf("bot info request failed: status=%d", httpResp.StatusCode)
		}
		var decoded botInfoHTTPResponse
		if err := json.NewDecoder(io.LimitReader(httpResp.Body, 1<<20)).Decode(&decoded); err != nil {
			return botInfoHTTPResponse{}, err
		}
		if decoded.Code != 0 {
			return botInfoHTTPResponse{}, fmt.Errorf("bot.v3.info failed: code=%d msg=%s", decoded.Code, decoded.Msg)
		}
		return decoded, nil
	})
	if err != nil {
		return BotInfo{}, err
	}
	return BotInfo{
		AppName: strings.TrimSpace(resp.Bot.AppName),
		OpenID:  strings.TrimSpace(resp.Bot.OpenID),
	}, nil
}

func getTenantAccessTokenHTTP(ctx context.Context, broker *FeishuCallBroker, client *http.Client, cfg LiveGatewayConfig) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"app_id":     strings.TrimSpace(cfg.AppID),
		"app_secret": strings.TrimSpace(cfg.AppSecret),
	})
	if err != nil {
		return "", err
	}
	resp, err := DoHTTP(ctx, broker, CallSpec{
		GatewayID:  cfg.GatewayID,
		API:        "auth.v3.tenant_access_token.internal",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetryOff,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, httpClient *http.Client) (tenantAccessTokenHTTPResponse, error) {
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, strings.TrimRight(feishuHTTPDomain(cfg), "/")+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(payload))
		if err != nil {
			return tenantAccessTokenHTTPResponse{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		httpResp, err := httpClient.Do(req)
		if err != nil {
			return tenantAccessTokenHTTPResponse{}, err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return tenantAccessTokenHTTPResponse{}, fmt.Errorf("tenant access token request failed: status=%d", httpResp.StatusCode)
		}
		var decoded tenantAccessTokenHTTPResponse
		if err := json.NewDecoder(io.LimitReader(httpResp.Body, 1<<20)).Decode(&decoded); err != nil {
			return tenantAccessTokenHTTPResponse{}, err
		}
		if decoded.Code != 0 || strings.TrimSpace(decoded.TenantAccessToken) == "" {
			return tenantAccessTokenHTTPResponse{}, fmt.Errorf("tenant access token failed: code=%d msg=%s", decoded.Code, decoded.Msg)
		}
		return decoded, nil
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.TenantAccessToken), nil
}

func feishuHTTPDomain(cfg LiveGatewayConfig) string {
	if strings.TrimSpace(cfg.Domain) != "" {
		return strings.TrimSpace(cfg.Domain)
	}
	return "https://open.feishu.cn"
}
