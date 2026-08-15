package singleturn

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func trim(value string) string {
	return strings.TrimSpace(value)
}

func trimURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func dataURL(image Image) string {
	return "data:" + image.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(image.Data)
}

// postJSON 发送 JSON 请求，返回响应体；非 2xx 时把响应体读入错误消息。
func postJSON(ctx context.Context, client *http.Client, url, apiKey string, headers map[string]string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("singleturn: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("singleturn: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("singleturn: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("singleturn: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("singleturn: status %d: %s", resp.StatusCode, errorMessageFromBody(body))
	}
	return body, nil
}

func errorMessageFromBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "empty error body"
	}
	var shape struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &shape) == nil && strings.TrimSpace(shape.Error.Message) != "" {
		return shape.Error.Message
	}
	if len(text) > 500 {
		text = text[:500] + "..."
	}
	return text
}
