package wrapper

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type predictionIdentity struct {
	UserID, AccountID, RoutingAccountID string
	Fedramp                             bool
}

type predictionRouting struct {
	ChatGPTAccountID       string `json:"chatgptAccountId"`
	BackendOrigin          string `json:"backendOrigin"`
	AccountRoutingOverride string `json:"accountRoutingOverride"`
}

type predictionConfig struct {
	Enabled           bool
	Prompt            string
	PromptWithSchema  string
	Effort            *string
	UnsupportedModels []string
	LiveSampleRate    float64
}

type predictionConfigCache struct {
	config  predictionConfig
	expires time.Time
}

var errPredictionConfigUnauthorized = errors.New("prediction configuration unauthorized")

func parsePredictionIdentity(token string) (predictionIdentity, error) {
	invalid := errors.New("native prediction identity unavailable")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return predictionIdentity{}, invalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return predictionIdentity{}, invalid
	}
	var claims struct {
		Exp  json.Number                `json:"exp"`
		Auth map[string]json.RawMessage `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return predictionIdentity{}, invalid
	}
	exp, err := claims.Exp.Int64()
	if err != nil || exp <= 0 {
		return predictionIdentity{}, invalid
	}
	readID := func(keys ...string) (string, bool) {
		for _, key := range keys {
			value, ok := claims.Auth[key]
			if !ok || string(value) == "null" {
				continue
			}
			var id string
			if json.Unmarshal(value, &id) != nil || strings.TrimSpace(id) == "" {
				return "", false
			}
			return id, true
		}
		return "", false
	}
	identity := predictionIdentity{}
	var ok bool
	identity.UserID, ok = readID("user_id", "chatgpt_user_id")
	if !ok {
		return predictionIdentity{}, invalid
	}
	identity.AccountID, ok = readID("chatgpt_account_id", "account_id")
	if !ok {
		return predictionIdentity{}, invalid
	}
	identity.RoutingAccountID, _ = readID("chatgpt_account_id")
	if value, exists := claims.Auth["chatgpt_account_is_fedramp"]; exists {
		if string(value) == "null" || json.Unmarshal(value, &identity.Fedramp) != nil {
			return predictionIdentity{}, invalid
		}
	}
	return identity, nil
}

func parsePredictionRouting(account, requirements json.RawMessage, identity predictionIdentity) (predictionRouting, error) {
	invalid := errors.New("native prediction workspace routing unavailable")
	var accountResult struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
		Routing *predictionRouting `json:"workspaceRouting"`
	}
	var requirementResult map[string]json.RawMessage
	if json.Unmarshal(account, &accountResult) != nil || accountResult.Account == nil || accountResult.Account.Type != "chatgpt" || accountResult.Routing == nil || json.Unmarshal(requirements, &requirementResult) != nil {
		// thread/sample requires a newer CLI than the desktop's 0.141–0.155 legacy routing window.
		return predictionRouting{}, invalid
	}
	raw, exists := requirementResult["requirements"]
	if !exists {
		return predictionRouting{}, invalid
	}
	if string(raw) != "null" {
		var constraints struct {
			BaseURL     *string `json:"chatgptBaseUrl"`
			Residency   *string `json:"enforceResidency"`
			Application *struct {
				Network json.RawMessage `json:"network"`
			} `json:"application"`
		}
		if json.Unmarshal(raw, &constraints) != nil {
			return predictionRouting{}, invalid
		}
	}
	routing := *accountResult.Routing
	origin, err := url.Parse(routing.BackendOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Scheme+"://"+origin.Host != routing.BackendOrigin || routing.ChatGPTAccountID == "" || routing.ChatGPTAccountID != identity.RoutingAccountID {
		return predictionRouting{}, invalid
	}
	switch routing.AccountRoutingOverride {
	case "NO_CONSTRAINT", "us", "us_cr", "eu_cr":
	default:
		return predictionRouting{}, invalid
	}
	return routing, nil
}

func parsePredictionConfig(raw []byte) (predictionConfig, error) {
	var config struct {
		Enabled           *bool           `json:"is_enabled"`
		Prompt            *string         `json:"prompt"`
		PromptWithSchema  json.RawMessage `json:"prompt_with_schema"`
		Version           *string         `json:"prompt_version"`
		Effort            *string         `json:"prediction_reasoning_effort"`
		UnsupportedModels json.RawMessage `json:"unsupported_models"`
		LiveSampleRate    json.RawMessage `json:"live_sample_rate"`
		CatchUpSampleRate json.RawMessage `json:"catch_up_sample_rate"`
	}
	if json.Unmarshal(raw, &config) != nil || config.Enabled == nil || config.Prompt == nil || config.Version == nil || config.Effort == nil {
		return predictionConfig{}, errors.New("invalid official prediction configuration")
	}
	var promptWithSchema string
	if json.Unmarshal(config.PromptWithSchema, &promptWithSchema) != nil {
		promptWithSchema = ""
	}
	if strings.TrimSpace(promptWithSchema) == "" && strings.TrimSpace(*config.Prompt) == "" {
		return predictionConfig{}, errors.New("invalid official prediction prompt")
	}
	rate := 1.0
	for _, value := range []json.RawMessage{config.LiveSampleRate, config.CatchUpSampleRate} {
		if len(value) == 0 {
			continue
		}
		var parsed float64
		if string(value) == "null" || json.Unmarshal(value, &parsed) != nil || parsed < 0 || parsed > 1 {
			return predictionConfig{}, errors.New("invalid official prediction sample rate")
		}
	}
	if len(config.LiveSampleRate) > 0 {
		_ = json.Unmarshal(config.LiveSampleRate, &rate)
	}
	var unsupported []string
	if len(config.UnsupportedModels) > 0 {
		var values []json.RawMessage
		if string(config.UnsupportedModels) == "null" || json.Unmarshal(config.UnsupportedModels, &values) != nil {
			return predictionConfig{}, errors.New("invalid official unsupported models")
		}
		for _, value := range values {
			var model string
			if string(value) == "null" || json.Unmarshal(value, &model) != nil {
				return predictionConfig{}, errors.New("invalid official unsupported model")
			}
			unsupported = append(unsupported, model)
		}
	}
	var effort *string
	if *config.Effort != "" {
		effort = config.Effort
	}
	return predictionConfig{Enabled: *config.Enabled, Prompt: *config.Prompt, PromptWithSchema: promptWithSchema, Effort: effort, UnsupportedModels: unsupported, LiveSampleRate: rate}, nil
}

// The cache stores configuration only; native tokens never enter it.
func (p *predictionRuntime) fetchConfig(ctx context.Context, token string, identity predictionIdentity, routing predictionRouting) (predictionConfig, error) {
	if cached, exists := p.cachedConfig(identity); exists {
		return cached, nil
	}
	return p.fetchConfigWithAuthScope(ctx, ctx, token, identity, routing)
}

func (p *predictionRuntime) cachedConfig(identity predictionIdentity) (predictionConfig, bool) {
	key := identity.UserID + "\x00" + identity.AccountID
	p.configMu.Lock()
	defer p.configMu.Unlock()
	cached, exists := p.cache[key]
	if exists && p.now().Before(cached.expires) {
		return cached.config, true
	}
	return predictionConfig{}, false
}

func (p *predictionRuntime) fetchConfigWithAuthScope(queryCtx, ctx context.Context, token string, identity predictionIdentity, routing predictionRouting) (config predictionConfig, err error) {
	defer func() {
		// Desktop getConfig catches ordinary fetch/parse failures as cached null.
		// A 401 is handled by the caller's one same-principal refresh first.
		if err != nil && queryCtx.Err() == nil && !errors.Is(err, errPredictionConfigUnauthorized) {
			_ = p.cacheConfig(queryCtx, identity, predictionConfig{})
		}
	}()
	raw, err := p.predictionBackendRequest(ctx, token, identity, routing, http.MethodGet, "/wham/predictions/config", nil)
	if err != nil {
		return predictionConfig{}, err
	}
	config, err = parsePredictionConfig(raw)
	if err != nil {
		return predictionConfig{}, err
	}
	if err = p.cacheConfig(queryCtx, identity, config); err != nil {
		return predictionConfig{}, err
	}
	return config, nil
}

func (p *predictionRuntime) predictionBackendRequest(ctx context.Context, token string, identity predictionIdentity, routing predictionRouting, method, path string, body []byte) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("CODEX_API_BASE_URL")), "/")
	if base == "" {
		base = "https://chatgpt.com/backend-api"
		if strings.EqualFold(os.Getenv("CODEX_API_ENDPOINT"), "localhost") {
			base = "http://localhost:8000/api"
		}
	}
	endpoint, err := url.Parse(base + path)
	if err != nil {
		return nil, errors.New("invalid prediction API base")
	}
	origin, err := url.Parse(routing.BackendOrigin)
	if err != nil {
		return nil, errors.New("invalid native prediction origin")
	}
	endpoint.Scheme, endpoint.Host, endpoint.User = origin.Scheme, origin.Host, nil
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid prediction config request")
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("ChatGPT-Account-Id", routing.ChatGPTAccountID)
	request.Header.Set("originator", "codex_desktop")
	if routing.AccountRoutingOverride != "NO_CONSTRAINT" {
		request.Header.Set("X-OpenAI-Account-Routing-Override", routing.AccountRoutingOverride)
	}
	if identity.Fedramp {
		request.Header.Set("X-OpenAI-Fedramp", "true")
	}
	// Never forward a native token to a redirect target, even on the same host.
	client := *p.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("official prediction config request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return nil, errPredictionConfigUnauthorized
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("official prediction config unavailable")
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, errors.New("official prediction config read failed")
	}
	return raw, nil
}

func (p *predictionRuntime) cacheConfig(ctx context.Context, identity predictionIdentity, config predictionConfig) error {
	p.configMu.Lock()
	defer p.configMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	p.cache[identity.UserID+"\x00"+identity.AccountID] = predictionConfigCache{config: config, expires: p.now().Add(5 * time.Minute)}
	return nil
}

type predictionRuntime struct {
	jobs     map[string]*predictionJob
	pending  map[string]chan predictionRPCResponse
	sequence uint64

	canceledRequests    [predictionCanceledRequestLimit]string
	nextCanceledRequest int

	configMu           sync.Mutex
	cache              map[string]predictionConfigCache
	controls           map[string]predictionControls
	httpClient         *http.Client
	now                func() time.Time
	sample             func() float64
	desktopContext     func(context.Context, string) (map[string]any, error)
	desktopPreferences func(context.Context, predictionIdentity) (predictionDesktopPreferences, error)
}
