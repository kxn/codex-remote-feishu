package desktopsession

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	statusAPIPath = "/api/admin/desktop-session/status"
	quitAPIPath   = "/api/admin/desktop-session/quit"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (c Client) Status(ctx context.Context) (Status, error) {
	endpoint, err := c.endpointURL(statusAPIPath)
	if err != nil {
		return Status{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Status{}, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("desktop session status returned %s", resp.Status)
	}
	var status Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return Status{}, err
	}
	if status.State == "" {
		status.State = StateNone
	}
	return status, nil
}

func (c Client) Quit(ctx context.Context) error {
	endpoint, err := c.endpointURL(quitAPIPath)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("desktop session quit returned %s", resp.Status)
	}
	return nil
}

func (c Client) endpointURL(path string) (string, error) {
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		return "", fmt.Errorf("desktop session base url is empty")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (c Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 2 * time.Second}
}
