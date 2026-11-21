package teamcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type Client struct{ HTTPClient *http.Client }

func NewClient() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Local work traffic must not expose Bearer tokens to proxies.
	return &Client{HTTPClient: &http.Client{Transport: transport, Timeout: 30 * time.Second}}
}

func localEndpoint(value string) (*url.URL, error) {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint == nil {
		return nil, fmt.Errorf("invalid work tool endpoint")
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("work tool endpoint requires http or https")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return nil, fmt.Errorf("work tool endpoint must not contain credentials, query, or fragment")
	}
	host := endpoint.Hostname()
	if strings.EqualFold(host, "localhost") {
		// Avoid DNS resolution for a credential-bearing local-only request.
		host = "127.0.0.1"
		if port := endpoint.Port(); port != "" {
			endpoint.Host = net.JoinHostPort(host, port)
		} else {
			endpoint.Host = host
		}
	} else {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("work tool endpoint must use a loopback address")
		}
	}
	if endpoint.Path != "" && endpoint.Path != "/" && endpoint.Path != workcmd.ExecutePath {
		return nil, fmt.Errorf("unsupported work tool endpoint path")
	}
	endpoint.Path = workcmd.ExecutePath
	endpoint.RawPath = ""
	return endpoint, nil
}

func (c *Client) Execute(ctx context.Context, endpointValue, token string, request workcmd.DispatchRequest) (json.RawMessage, error) {
	endpoint, err := localEndpoint(endpointValue)
	if err != nil {
		return nil, err
	}
	if token == "" || strings.ContainsAny(token, "\r\n\t ") {
		return nil, fmt.Errorf("invalid bound work session credential")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("cannot encode team command")
	}
	if len(body) > workcmd.MaxRequestBytes {
		return nil, fmt.Errorf("encoded team command exceeds size limit")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cannot construct team command request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	base := c.HTTPClient
	if base == nil {
		base = NewClient().HTTPClient
	}
	httpClient := *base
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := httpClient.Do(req)
	if err != nil {
		// Transport errors can contain URLs or caller-supplied diagnostics. Keep the
		// credential out of output even when a custom transport embeds it in an error.
		message := strings.ReplaceAll(err.Error(), token, "[redacted]")
		return nil, fmt.Errorf("team command transport failed: %s", message)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, workcmd.MaxRequestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read team command response")
	}
	if len(data) > workcmd.MaxRequestBytes {
		return nil, fmt.Errorf("team command response exceeds size limit")
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *workcmd.Error  `json:"error"`
	}
	if err := workcmd.ValidateJSON(data); err != nil {
		return nil, fmt.Errorf("invalid team command response")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("invalid team command response")
	}
	if envelope.Error != nil {
		envelope.Error.Message = strings.ReplaceAll(envelope.Error.Message, token, "[redacted]")
		envelope.Error.Code = strings.ReplaceAll(envelope.Error.Code, token, "[redacted]")
		return nil, envelope.Error
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("team command HTTP status %d", resp.StatusCode)
	}
	if len(envelope.Result) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Result), []byte("null")) {
		return nil, fmt.Errorf("missing team command result")
	}
	return envelope.Result, nil
}
