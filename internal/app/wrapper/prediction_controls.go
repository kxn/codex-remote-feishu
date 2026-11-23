package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type predictionControls struct {
	Evaluated         bool
	InterruptOnCancel bool
	ModerationPolicy  json.RawMessage
}

// Read the same authenticated bootstrap as the desktop. Values are never baked
// into Remote: the first prediction in each new process obtains its evaluation.
func (p *predictionRuntime) fetchControls(ctx context.Context, token string, identity predictionIdentity, routing predictionRouting, desktop map[string]any) (predictionControls, error) {
	body, err := json.Marshal(desktop)
	if err != nil {
		return predictionControls{}, errors.New("invalid desktop prediction context")
	}
	raw, err := p.predictionBackendRequest(ctx, token, identity, routing, "POST", "/wham/statsig/bootstrap", body)
	if err != nil {
		return predictionControls{}, err
	}
	return parsePredictionControls(raw, identity)
}

func parsePredictionControls(raw []byte, identity predictionIdentity) (predictionControls, error) {
	invalid := errors.New("official desktop prediction evaluations unavailable")
	var outer struct {
		Payload *string `json:"statsigPayload"`
	}
	if json.Unmarshal(raw, &outer) != nil || outer.Payload == nil {
		return predictionControls{}, invalid
	}
	var payload struct {
		Hash string `json:"hash_used"`
		User struct {
			UserID    string            `json:"userID"`
			CustomIDs map[string]string `json:"customIDs"`
		} `json:"user"`
		Gates map[string]struct {
			Value json.RawMessage `json:"value"`
		} `json:"feature_gates"`
		Configs map[string]struct {
			Value json.RawMessage `json:"value"`
		} `json:"dynamic_configs"`
	}
	if json.Unmarshal([]byte(*outer.Payload), &payload) != nil || payload.User.UserID != identity.UserID || payload.User.CustomIDs["account_id"] != identity.AccountID {
		return predictionControls{}, invalid
	}
	key := func(name string) string { return name }
	switch payload.Hash {
	case "", "none":
	case "djb2":
		// The desktop SDK calls this DJB2; its exact implementation is h=31*h+c.
		key = func(name string) string {
			var h uint32
			for _, c := range name {
				h = h*31 + uint32(c)
			}
			return strconv.FormatUint(uint64(h), 10)
		}
	default:
		return predictionControls{}, invalid
	}
	return predictionControls{
		Evaluated:         true,
		InterruptOnCancel: bytes.Equal(bytes.TrimSpace(payload.Gates[key("codex-app-cumberbatch-interrupt-on-cancel")].Value), []byte("true")),
		ModerationPolicy:  payload.Configs[key("codex-app-cumberbatch-mod-policy")].Value,
	}, nil
}

// Native initialization is consumed by headless bootstrap before ObserveServer.
func (r *codexBackendRuntime) observePredictionInitialization(raw []byte) {
	var frame struct {
		Result struct {
			UserAgent string `json:"userAgent"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &frame) != nil {
		return
	}
	r.mu.Lock()
	r.nativeUserAgent = frame.Result.UserAgent
	r.mu.Unlock()
}

func predictionInterruptSupported(userAgent string) bool {
	client, versionInfo, ok := strings.Cut(userAgent, "/")
	if !ok || (client != "Codex Desktop" && client != "Codex Remote Headless") {
		return false
	}
	fields := strings.Fields(versionInfo)
	if len(fields) == 0 {
		return false
	}
	version, _, _ := strings.Cut(fields[0], "+")
	base, preview, hasPreview := strings.Cut(version, "-")
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return false
	}
	minimum := []uint64{0, 167, 0}
	var numbers [3]uint64
	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return false
		}
		numbers[i] = n
	}
	for i, n := range numbers {
		if n != minimum[i] {
			return n > minimum[i]
		}
	}
	if !hasPreview {
		return true
	}
	ids := strings.Split(preview, ".")
	if ids[0] != "alpha" {
		return ids[0] > "alpha"
	}
	if len(ids) < 2 {
		return false
	}
	n, err := strconv.ParseUint(ids[1], 10, 64)
	return err != nil || n >= 1
}

func (r *codexBackendRuntime) predictionControlsForJob(job *predictionJob, token string, identity predictionIdentity, routing predictionRouting) predictionControls {
	p := r.predictions
	cacheKey := identity.UserID + "\x00" + identity.AccountID
	p.configMu.Lock()
	cached, exists := p.controls[cacheKey]
	p.configMu.Unlock()
	if exists {
		return cached
	}
	if p.desktopContext == nil {
		return predictionControls{}
	}
	desktop, err := p.desktopContext(job.ctx, identity.AccountID)
	if err != nil {
		return predictionControls{}
	}
	r.mu.Lock()
	if job.ctx.Err() != nil || p.jobs[job.command.CommandID] != job {
		r.mu.Unlock()
		return predictionControls{}
	}
	authCtx, authCancel := context.WithCancel(job.ctx)
	job.authCancel = authCancel
	r.mu.Unlock()
	defer func() { authCancel(); r.mu.Lock(); job.authCancel = nil; r.mu.Unlock() }()
	controls, err := p.fetchControls(authCtx, token, identity, routing, desktop)
	if errors.Is(err, errPredictionConfigUnauthorized) {
		refreshedToken, refreshed, refreshErr := r.predictionAuthToken(authCtx, job, true)
		if refreshErr == nil && refreshed.UserID == identity.UserID && refreshed.AccountID == identity.AccountID && refreshed.RoutingAccountID == routing.ChatGPTAccountID {
			controls, err = p.fetchControls(authCtx, refreshedToken, refreshed, routing, desktop)
		}
	}
	if err != nil || authCtx.Err() != nil || job.ctx.Err() != nil {
		return predictionControls{}
	}
	p.configMu.Lock()
	if authCtx.Err() != nil || job.ctx.Err() != nil {
		p.configMu.Unlock()
		return predictionControls{}
	}
	if p.controls == nil {
		p.controls = make(map[string]predictionControls)
	}
	p.controls[cacheKey] = controls
	p.configMu.Unlock()
	return controls
}
