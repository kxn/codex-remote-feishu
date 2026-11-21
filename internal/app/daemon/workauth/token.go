// Package workauth authenticates a worker independently of the selected chat.
// A signature authenticates immutable claims, not current permission: every
// request must also resolve and validate the live worker/attempt in the work store.
package workauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("invalid or expired work credential")

const tokenVersion = 1
const maxTokenBytes = 8192

// Claims contain no mutable current-task selection or user-selected bot identity.
// The bootstrap purpose permits only project establishment for its one chat.
type Claims struct {
	Version              int    `json:"v"`
	Purpose              string `json:"purpose"`
	RoleID               string `json:"role"`
	BotGeneration        int64  `json:"bot_generation"`
	ScopeID              string `json:"scope,omitempty"`
	ContextID            string `json:"context,omitempty"`
	WorkerID             string `json:"worker,omitempty"`
	SessionGeneration    int64  `json:"session_generation,omitempty"`
	ControllerGeneration int64  `json:"controller_generation,omitempty"`
	Tenant               string `json:"tenant,omitempty"`
	ChatID               string `json:"chat,omitempty"`
	PrincipalID          string `json:"principal,omitempty"`
	OperationID          string `json:"operation,omitempty"`
	Nonce                string `json:"nonce"`
	IssuedAt             int64  `json:"iat"`
	ExpiresAt            int64  `json:"exp"`
}

func (c Claims) validate(now time.Time) error {
	if c.Version != tokenVersion || c.RoleID == "" || c.BotGeneration < 1 || c.Nonce == "" || c.IssuedAt > now.Unix() || c.ExpiresAt <= now.Unix() || c.ExpiresAt <= c.IssuedAt {
		return ErrUnauthorized
	}
	switch c.Purpose {
	case "worker":
		if c.ScopeID == "" || c.ContextID == "" || c.WorkerID == "" || c.SessionGeneration < 1 || c.ControllerGeneration < 1 || c.ChatID != "" || c.OperationID != "" {
			return ErrUnauthorized
		}
	case "bootstrap":
		if c.Tenant == "" || c.ChatID == "" || c.PrincipalID == "" || c.OperationID == "" || c.WorkerID != "" || c.ScopeID != "" || c.ContextID != "" || c.SessionGeneration != 0 || c.ControllerGeneration != 0 {
			return ErrUnauthorized
		}
	default:
		return ErrUnauthorized
	}
	return nil
}

type Signer struct {
	key []byte
	now func() time.Time
}

func New(key []byte, now func() time.Time) (*Signer, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("work signing key must contain at least 32 random bytes")
	}
	if now == nil {
		now = time.Now
	}
	return &Signer{key: append([]byte(nil), key...), now: now}, nil
}

// Issue never infers scope or a current run. Run references travel separately
// with each request so a late command cannot acquire the next attempt's identity.
func (s *Signer) Issue(c Claims, ttl time.Duration) (string, error) {
	if ttl < time.Second {
		return "", fmt.Errorf("work credential lifetime must be positive")
	}
	now := s.now()
	c.Version = tokenVersion
	c.IssuedAt = now.Unix()
	c.ExpiresAt = now.Add(ttl).Unix()
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	c.Nonce = base64.RawURLEncoding.EncodeToString(nonce)
	if err := c.validate(now); err != nil {
		return "", err
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Signer) Verify(token string) (Claims, error) {
	if len(token) == 0 || len(token) > maxTokenBytes {
		return Claims{}, ErrUnauthorized
	}
	body, signature, ok := strings.Cut(token, ".")
	if !ok {
		return Claims{}, ErrUnauthorized
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return Claims{}, ErrUnauthorized
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(body))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return Claims{}, ErrUnauthorized
	}
	data, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Claims{}, ErrUnauthorized
	}
	var c Claims
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return Claims{}, ErrUnauthorized
	}
	if err = c.validate(s.now()); err != nil {
		return Claims{}, err
	}
	return c, nil
}
