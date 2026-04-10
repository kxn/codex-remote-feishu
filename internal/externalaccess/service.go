package externalaccess

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const defaultCookieName = "codex_remote_external_access"

var (
	ErrDisabled             = errors.New("external access disabled")
	ErrInvalidTargetURL     = errors.New("invalid target url")
	ErrTargetNotLoopback    = errors.New("target url must resolve to loopback")
	ErrGrantNotFound        = errors.New("grant not found")
	ErrGrantExpired         = errors.New("grant expired")
	ErrInvalidExchange      = errors.New("invalid exchange token")
	ErrInvalidSession       = errors.New("invalid external access session")
	ErrSessionExpired       = errors.New("external access session expired")
	ErrPathOutsideAllowlist = errors.New("request path outside allowlist")
)

type Purpose string

const (
	PurposePreview Purpose = "preview"
	PurposeReview  Purpose = "review"
	PurposeDebug   Purpose = "debug"
)

type IssueRequest struct {
	Purpose        Purpose
	TargetURL      string
	TargetBasePath string
	LinkTTL        time.Duration
	SessionTTL     time.Duration
	AllowWebsocket bool
}

type IssuedURL struct {
	ExternalURL  string
	ProviderKind string
	ExpiresAt    time.Time
}

type PublicBase struct {
	BaseURL   string
	StartedAt time.Time
}

type Provider interface {
	Kind() string
	EnsurePublicBase(context.Context, string) (PublicBase, error)
	Snapshot() ProviderStatus
	Close() error
}

type ProviderStatus struct {
	Kind      string    `json:"kind,omitempty"`
	BaseURL   string    `json:"baseURL,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Ready     bool      `json:"ready,omitempty"`
	LastError string    `json:"lastError,omitempty"`
}

type Options struct {
	Now               func() time.Time
	Provider          Provider
	DefaultLinkTTL    time.Duration
	DefaultSessionTTL time.Duration
	IdleTTL           time.Duration
	CookieName        string
}

type Grant struct {
	ID             string        `json:"id"`
	Purpose        Purpose       `json:"purpose"`
	TargetURL      string        `json:"targetURL"`
	TargetBasePath string        `json:"targetBasePath"`
	AllowWebsocket bool          `json:"allowWebsocket"`
	IssuedAt       time.Time     `json:"issuedAt"`
	ExpiresAt      time.Time     `json:"expiresAt"`
	SessionTTL     time.Duration `json:"sessionTTL"`
}

type Status struct {
	Enabled        bool           `json:"enabled"`
	CookieName     string         `json:"cookieName,omitempty"`
	ListenerURL    string         `json:"listenerURL,omitempty"`
	ListenerActive bool           `json:"listenerActive"`
	Provider       ProviderStatus `json:"provider"`
	GrantCount     int            `json:"grantCount"`
	SessionCount   int            `json:"sessionCount"`
	LastInboundAt  *time.Time     `json:"lastInboundAt,omitempty"`
	LastOutboundAt *time.Time     `json:"lastOutboundAt,omitempty"`
	LastActivityAt *time.Time     `json:"lastActivityAt,omitempty"`
	IdleTTL        time.Duration  `json:"idleTTL"`
	IdleDeadlineAt *time.Time     `json:"idleDeadlineAt,omitempty"`
	IdleRemaining  time.Duration  `json:"idleRemaining,omitempty"`
	ActiveGrants   []Grant        `json:"activeGrants,omitempty"`
}

type Service struct {
	now               func() time.Time
	provider          Provider
	defaultLinkTTL    time.Duration
	defaultSessionTTL time.Duration
	idleTTL           time.Duration
	cookieName        string

	mu             sync.Mutex
	listenerURL    string
	listenerActive bool
	publicBase     PublicBase
	grants         map[string]*grantRecord
	sessions       map[string]*sessionRecord
	lastInboundAt  time.Time
	lastOutboundAt time.Time
	lastActivityAt time.Time
}

type grantRecord struct {
	Grant
	target          *url.URL
	exchangeToken   string
	exchangeExpires time.Time
}

type sessionRecord struct {
	GrantID   string
	ExpiresAt time.Time
}

func NewService(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	linkTTL := opts.DefaultLinkTTL
	if linkTTL <= 0 {
		linkTTL = 10 * time.Minute
	}
	sessionTTL := opts.DefaultSessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 30 * time.Minute
	}
	idleTTL := opts.IdleTTL
	if idleTTL <= 0 {
		idleTTL = 5 * time.Minute
	}
	cookieName := strings.TrimSpace(opts.CookieName)
	if cookieName == "" {
		cookieName = defaultCookieName
	}
	return &Service{
		now:               now,
		provider:          opts.Provider,
		defaultLinkTTL:    linkTTL,
		defaultSessionTTL: sessionTTL,
		idleTTL:           idleTTL,
		cookieName:        cookieName,
		grants:            map[string]*grantRecord{},
		sessions:          map[string]*sessionRecord{},
	}
}

func (s *Service) SetListenerState(listenerURL string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listenerURL = strings.TrimSpace(listenerURL)
	s.listenerActive = active
}

func (s *Service) IssueURL(ctx context.Context, req IssueRequest, localListenerURL string) (IssuedURL, error) {
	localListenerURL = strings.TrimSpace(localListenerURL)
	if localListenerURL == "" {
		return IssuedURL{}, ErrDisabled
	}
	target, basePath, err := validateIssueRequest(req)
	if err != nil {
		return IssuedURL{}, err
	}
	linkTTL := req.LinkTTL
	if linkTTL <= 0 {
		linkTTL = s.defaultLinkTTL
	}
	sessionTTL := req.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = s.defaultSessionTTL
	}

	baseURL, providerKind, err := s.ensurePublicBase(ctx, localListenerURL)
	if err != nil {
		return IssuedURL{}, err
	}

	now := s.now().UTC()
	record := &grantRecord{
		Grant: Grant{
			ID:             randomToken(18),
			Purpose:        req.Purpose,
			TargetURL:      target.String(),
			TargetBasePath: basePath,
			AllowWebsocket: req.AllowWebsocket,
			IssuedAt:       now,
			ExpiresAt:      now.Add(linkTTL),
			SessionTTL:     sessionTTL,
		},
		target: target,
	}
	record.exchangeToken = randomToken(24)
	record.exchangeExpires = record.ExpiresAt

	s.mu.Lock()
	defer s.mu.Unlock()
	s.reapExpiredLocked(now)
	s.grants[record.ID] = record

	return IssuedURL{
		ExternalURL:  strings.TrimRight(baseURL, "/") + "/g/" + record.ID + "/?t=" + url.QueryEscape(record.exchangeToken),
		ProviderKind: providerKind,
		ExpiresAt:    record.ExpiresAt,
	}, nil
}

func (s *Service) Snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	s.reapExpiredLocked(now)
	status := Status{
		Enabled:        true,
		CookieName:     s.cookieName,
		ListenerURL:    s.listenerURL,
		ListenerActive: s.listenerActive,
		IdleTTL:        s.idleTTL,
		GrantCount:     len(s.grants),
		SessionCount:   len(s.sessions),
		Provider:       s.providerStatusLocked(),
	}
	if !s.lastInboundAt.IsZero() {
		value := s.lastInboundAt
		status.LastInboundAt = &value
	}
	if !s.lastOutboundAt.IsZero() {
		value := s.lastOutboundAt
		status.LastOutboundAt = &value
	}
	if !s.lastActivityAt.IsZero() {
		value := s.lastActivityAt
		status.LastActivityAt = &value
		deadline := s.lastActivityAt.Add(s.idleTTL)
		status.IdleDeadlineAt = &deadline
		status.IdleRemaining = maxDuration(0, time.Until(deadline))
	}
	status.ActiveGrants = make([]Grant, 0, len(s.grants))
	for _, grant := range s.grants {
		status.ActiveGrants = append(status.ActiveGrants, grant.Grant)
	}
	return status
}

func (s *Service) IdleExpired(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.listenerActive || s.lastActivityAt.IsZero() {
		return false
	}
	return !now.UTC().Before(s.lastActivityAt.Add(s.idleTTL))
}

func (s *Service) ShutdownRuntime() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listenerActive = false
	s.listenerURL = ""
	s.publicBase = PublicBase{}
	s.grants = map[string]*grantRecord{}
	s.sessions = map[string]*sessionRecord{}
	s.lastInboundAt = time.Time{}
	s.lastOutboundAt = time.Time{}
	s.lastActivityAt = time.Time{}
	if s.provider == nil {
		return nil
	}
	return s.provider.Close()
}

func (s *Service) Close() error {
	return s.ShutdownRuntime()
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := s.now().UTC()
	s.noteInbound(now)

	grantID, remainder, ok := parseGrantPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if remainder == "" {
		http.Redirect(w, r, "/g/"+grantID+"/", http.StatusFound)
		return
	}
	if token := strings.TrimSpace(r.URL.Query().Get("t")); token != "" {
		s.handleExchange(w, r, grantID, token)
		return
	}
	grant, err := s.authorizeRequest(r, grantID)
	if err != nil {
		writeUnauthorized(w, err)
		return
	}
	targetURL, err := upstreamTargetURL(grant, remainder, r.URL.RawQuery)
	if err != nil {
		writeForbidden(w, err)
		return
	}
	s.noteOutbound(now)
	prefix := "/g/" + grant.ID + "/"
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = targetURL.Scheme
			pr.Out.URL.Host = targetURL.Host
			pr.Out.URL.Path = targetURL.Path
			pr.Out.URL.RawPath = targetURL.RawPath
			pr.Out.URL.RawQuery = targetURL.RawQuery
			pr.Out.Host = targetURL.Host
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Prefix", prefix)
		},
		ModifyResponse: func(resp *http.Response) error {
			return rewriteProxyResponse(resp, grant, prefix)
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			writeBadGateway(rw, err)
		},
	}
	proxy.ServeHTTP(w, r)
}

func (s *Service) handleExchange(w http.ResponseWriter, r *http.Request, grantID, token string) {
	grant, session, err := s.exchange(grantID, token)
	if err != nil {
		writeUnauthorized(w, err)
		return
	}
	expiresAt := session.ExpiresAt
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    session.Token,
		Path:     "/g/" + grant.ID + "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
		Expires:  expiresAt,
		MaxAge:   maxInt(0, int(time.Until(expiresAt).Seconds())),
	})
	redirectURL := &url.URL{Path: r.URL.Path}
	query := r.URL.Query()
	query.Del("t")
	redirectURL.RawQuery = query.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

func (s *Service) authorizeRequest(r *http.Request, grantID string) (*grantRecord, error) {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil {
		return nil, ErrInvalidSession
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reapExpiredLocked(now)
	session := s.sessions[cookie.Value]
	if session == nil {
		return nil, ErrInvalidSession
	}
	if session.GrantID != grantID {
		return nil, ErrInvalidSession
	}
	if !session.ExpiresAt.After(now) {
		delete(s.sessions, cookie.Value)
		return nil, ErrSessionExpired
	}
	grant := s.grants[grantID]
	if grant == nil {
		return nil, ErrGrantNotFound
	}
	return cloneGrant(grant), nil
}

func (s *Service) exchange(grantID, token string) (*grantRecord, sessionValue, error) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reapExpiredLocked(now)
	grant := s.grants[grantID]
	if grant == nil {
		return nil, sessionValue{}, ErrGrantNotFound
	}
	if !grant.exchangeExpires.After(now) {
		return nil, sessionValue{}, ErrGrantExpired
	}
	if subtleCompare(grant.exchangeToken, token) != 1 {
		return nil, sessionValue{}, ErrInvalidExchange
	}
	value := sessionValue{
		Token:     randomToken(24),
		ExpiresAt: now.Add(grant.SessionTTL),
	}
	s.sessions[value.Token] = &sessionRecord{GrantID: grantID, ExpiresAt: value.ExpiresAt}
	return cloneGrant(grant), value, nil
}

type sessionValue struct {
	Token     string
	ExpiresAt time.Time
}

func (s *Service) ensurePublicBase(ctx context.Context, localListenerURL string) (string, string, error) {
	s.mu.Lock()
	if strings.TrimSpace(s.publicBase.BaseURL) != "" {
		baseURL := s.publicBase.BaseURL
		kind := "local"
		if s.provider != nil {
			kind = s.provider.Kind()
		}
		s.mu.Unlock()
		return baseURL, kind, nil
	}
	s.mu.Unlock()

	if s.provider == nil {
		s.mu.Lock()
		s.publicBase = PublicBase{BaseURL: localListenerURL, StartedAt: s.now().UTC()}
		s.mu.Unlock()
		return localListenerURL, "local", nil
	}
	publicBase, err := s.provider.EnsurePublicBase(ctx, localListenerURL)
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	s.publicBase = publicBase
	s.mu.Unlock()
	return publicBase.BaseURL, s.provider.Kind(), nil
}

func (s *Service) providerStatusLocked() ProviderStatus {
	if s.provider == nil {
		if strings.TrimSpace(s.publicBase.BaseURL) == "" {
			return ProviderStatus{Kind: "local"}
		}
		return ProviderStatus{Kind: "local", BaseURL: s.publicBase.BaseURL, StartedAt: s.publicBase.StartedAt, Ready: true}
	}
	return s.provider.Snapshot()
}

func (s *Service) noteInbound(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastInboundAt = now
	s.lastActivityAt = now
}

func (s *Service) noteOutbound(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastOutboundAt = now
	s.lastActivityAt = now
}

func (s *Service) reapExpiredLocked(now time.Time) {
	for key, value := range s.grants {
		if !value.ExpiresAt.After(now) {
			delete(s.grants, key)
		}
	}
	for key, value := range s.sessions {
		if !value.ExpiresAt.After(now) {
			delete(s.sessions, key)
		}
	}
}

func validateIssueRequest(req IssueRequest) (*url.URL, string, error) {
	targetText := strings.TrimSpace(req.TargetURL)
	if targetText == "" {
		return nil, "", ErrInvalidTargetURL
	}
	target, err := url.Parse(targetText)
	if err != nil {
		return nil, "", ErrInvalidTargetURL
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, "", ErrInvalidTargetURL
	}
	if target.Host == "" {
		return nil, "", ErrInvalidTargetURL
	}
	host := target.Hostname()
	if !isLoopbackHost(host) {
		return nil, "", ErrTargetNotLoopback
	}
	target.Path = normalizeURLPath(target.Path)
	basePath := normalizeBasePath(req.TargetBasePath)
	if basePath == "" {
		basePath = deriveBasePath(target.Path)
	}
	return target, basePath, nil
}

func upstreamTargetURL(grant *grantRecord, remainder, rawQuery string) (*url.URL, error) {
	upstream := cloneURL(grant.target)
	remainder = ensureLeadingSlash(remainder)
	if remainder == "/" {
		upstream.Path = normalizeURLPath(grant.target.Path)
		upstream.RawQuery = combineQuery(grant.target.RawQuery, rawQuery)
		return upstream, nil
	}
	if strings.Contains(remainder, "..") {
		return nil, ErrPathOutsideAllowlist
	}
	targetPath := joinURLPath(grant.TargetBasePath, remainder)
	if !pathWithinBase(targetPath, grant.TargetBasePath) {
		return nil, ErrPathOutsideAllowlist
	}
	upstream.Path = targetPath
	upstream.RawQuery = rawQuery
	return upstream, nil
}

func rewriteProxyResponse(resp *http.Response, grant *grantRecord, prefix string) error {
	if resp == nil {
		return nil
	}
	if location := strings.TrimSpace(resp.Header.Get("Location")); location != "" {
		rewritten, err := rewriteLocation(location, resp.Request.URL, grant, prefix)
		if err != nil {
			return err
		}
		resp.Header.Set("Location", rewritten)
	}
	cookies := resp.Cookies()
	if len(cookies) > 0 {
		resp.Header.Del("Set-Cookie")
		for _, cookie := range cookies {
			if cookie == nil {
				continue
			}
			cookie.Path = prefix
			cookie.Domain = ""
			resp.Header.Add("Set-Cookie", cookie.String())
		}
	}
	return nil
}

func rewriteLocation(location string, requestURL *url.URL, grant *grantRecord, prefix string) (string, error) {
	parsed, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	resolved := requestURL.ResolveReference(parsed)
	if !sameOrigin(resolved, grant.target) || !pathWithinBase(resolved.Path, grant.TargetBasePath) {
		return "", ErrPathOutsideAllowlist
	}
	external := &url.URL{Path: externalPathForUpstream(grant, resolved.Path, prefix), RawQuery: resolved.RawQuery, Fragment: resolved.Fragment}
	return external.String(), nil
}

func externalPathForUpstream(grant *grantRecord, upstreamPath, prefix string) string {
	upstreamPath = normalizeURLPath(upstreamPath)
	if upstreamPath == normalizeURLPath(grant.target.Path) {
		return prefix
	}
	base := grant.TargetBasePath
	if base == "/" {
		return path.Join(strings.TrimRight(prefix, "/"), strings.TrimPrefix(upstreamPath, "/")) + trailingSlashFor(upstreamPath)
	}
	trimmed := strings.TrimPrefix(upstreamPath, strings.TrimRight(base, "/"))
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return prefix
	}
	return path.Join(strings.TrimRight(prefix, "/"), trimmed) + trailingSlashFor(upstreamPath)
}

func parseGrantPath(pathValue string) (string, string, bool) {
	if !strings.HasPrefix(pathValue, "/g/") {
		return "", "", false
	}
	trimmed := strings.TrimPrefix(pathValue, "/g/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", false
	}
	remainder := "/"
	if len(parts) == 2 {
		remainder = "/" + parts[1]
	}
	return parts[0], remainder, true
}

func normalizeBasePath(value string) string {
	value = ensureLeadingSlash(strings.TrimSpace(value))
	if value == "" || value == "." {
		return ""
	}
	if !strings.HasSuffix(value, "/") {
		value += "/"
	}
	return path.Clean(value) + "/"
}

func deriveBasePath(targetPath string) string {
	targetPath = normalizeURLPath(targetPath)
	switch {
	case targetPath == "/":
		return "/"
	case strings.HasSuffix(targetPath, "/"):
		return targetPath
	case path.Ext(targetPath) != "":
		dir := path.Dir(targetPath)
		if dir == "." || dir == "/" {
			return "/"
		}
		return ensureLeadingSlash(dir) + "/"
	default:
		return targetPath + "/"
	}
}

func normalizeURLPath(value string) string {
	value = ensureLeadingSlash(strings.TrimSpace(value))
	if value == "" {
		return "/"
	}
	cleaned := path.Clean(value)
	if cleaned == "." {
		return "/"
	}
	if strings.HasSuffix(value, "/") && !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	return ensureLeadingSlash(cleaned)
}

func ensureLeadingSlash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return value
}

func joinURLPath(basePath, remainder string) string {
	basePath = normalizeBasePath(basePath)
	remainder = strings.TrimPrefix(ensureLeadingSlash(remainder), "/")
	if basePath == "/" {
		return normalizeURLPath("/" + remainder)
	}
	return normalizeURLPath(strings.TrimRight(basePath, "/") + "/" + remainder)
}

func pathWithinBase(targetPath, basePath string) bool {
	targetPath = normalizeURLPath(targetPath)
	basePath = normalizeBasePath(basePath)
	if basePath == "/" {
		return true
	}
	return targetPath == strings.TrimRight(basePath, "/") || strings.HasPrefix(targetPath, basePath)
}

func combineQuery(first, second string) string {
	first = strings.TrimSpace(first)
	second = strings.TrimSpace(second)
	switch {
	case first == "":
		return second
	case second == "":
		return first
	default:
		return first + "&" + second
	}
}

func cloneURL(value *url.URL) *url.URL {
	if value == nil {
		return &url.URL{}
	}
	copied := *value
	return &copied
}

func cloneGrant(value *grantRecord) *grantRecord {
	if value == nil {
		return nil
	}
	copied := *value
	copied.target = cloneURL(value.target)
	return &copied
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func randomToken(size int) string {
	if size <= 0 {
		size = 16
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func subtleCompare(left, right string) int {
	if len(left) != len(right) {
		return 0
	}
	return subtleConstantTimeCompare([]byte(left), []byte(right))
}

func subtleConstantTimeCompare(left, right []byte) int {
	if len(left) != len(right) {
		return 0
	}
	result := byte(0)
	for i := range left {
		result |= left[i] ^ right[i]
	}
	if result == 0 {
		return 1
	}
	return 0
}

func requestIsSecure(r *http.Request) bool {
	if r != nil && r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func trailingSlashFor(value string) string {
	if strings.HasSuffix(value, "/") {
		return "/"
	}
	return ""
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func writeUnauthorized(w http.ResponseWriter, err error) {
	status := http.StatusUnauthorized
	message := "external access authorization failed"
	switch {
	case errors.Is(err, ErrGrantNotFound), errors.Is(err, ErrGrantExpired):
		status = http.StatusGone
		message = err.Error()
	case errors.Is(err, ErrInvalidExchange), errors.Is(err, ErrInvalidSession), errors.Is(err, ErrSessionExpired):
		message = err.Error()
	}
	http.Error(w, message, status)
}

func writeForbidden(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusForbidden)
}

func writeBadGateway(w http.ResponseWriter, err error) {
	http.Error(w, fmt.Sprintf("bad gateway: %v", err), http.StatusBadGateway)
}

func ResolveBundledCloudflaredPath(currentBinaryPath string) string {
	currentBinaryPath = strings.TrimSpace(currentBinaryPath)
	if currentBinaryPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(currentBinaryPath), executableName("cloudflared"))
}

func executableName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}
