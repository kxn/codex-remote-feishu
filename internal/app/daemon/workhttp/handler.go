// Package workhttp exposes a bounded transport for the shared team dispatcher.
// It never opens a listener and never owns task or execution state.
package workhttp

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type response struct {
	Result any            `json:"result,omitempty"`
	Error  *workcmd.Error `json:"error,omitempty"`
}

// NewHandler is intended for an existing loopback gateway mux. The peer check
// also refuses requests if accidentally mounted on a public listener. Proxy or
// forwarded headers cannot change the authenticated network peer.
func NewHandler(dispatcher workcmd.Dispatcher) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			writeError(w, http.StatusForbidden, "local_only", "Team commands require a local connection.")
			return
		}
		if r.URL.Path != workcmd.ExecutePath {
			writeError(w, http.StatusNotFound, "not_found", "Unknown team endpoint.")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST for team commands.")
			return
		}
		headers := r.Header.Values("Authorization")
		if len(headers) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "A work session credential is required.")
			return
		}
		parts := strings.Fields(headers[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "A work session credential is required.")
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "invalid_argument", "Content-Type must be application/json.")
			return
		}
		reader := http.MaxBytesReader(w, r.Body, workcmd.MaxRequestBytes)
		body, err := io.ReadAll(reader)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Team command body exceeds the size limit.")
			} else {
				writeError(w, http.StatusBadRequest, "invalid_argument", "Cannot read team command body.")
			}
			return
		}
		request, err := workcmd.DecodeRequest(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_argument", redact(err.Error(), parts[1]))
			return
		}
		if dispatcher == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "Team command service is unavailable.")
			return
		}
		result, err := dispatcher.Execute(r.Context(), parts[1], request)
		if err != nil {
			writeDispatchError(w, err, parts[1])
			return
		}
		// Encode before writing status: an unserializable service result must not
		// leave a partial successful response on the wire.
		encoded, err := json.Marshal(response{Result: result})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Cannot encode team command result.")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append(encoded, '\n'))
	})
}

func redact(message, token string) string {
	if token != "" {
		return strings.ReplaceAll(message, token, "[redacted]")
	}
	return message
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response{Error: &workcmd.Error{Code: code, Message: message}})
}
func writeDispatchError(w http.ResponseWriter, err error, token string) {
	switch {
	case errors.Is(err, workauth.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized", "The work session or run reference is no longer authorized.")
	case errors.Is(err, workstore.ErrScopeDenied):
		writeError(w, http.StatusForbidden, "forbidden", "The requested object is outside the authorized scope.")
	case errors.Is(err, workstore.ErrConflict), errors.Is(err, workstore.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "conflict", "The object changed or the idempotency key conflicts.")
	case errors.Is(err, workstore.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "The requested work object was not found.")
	default:
		var commandError *workcmd.Error
		if errors.As(err, &commandError) {
			status := http.StatusBadRequest
			switch commandError.Code {
			case "unauthorized":
				status = http.StatusUnauthorized
			case "forbidden":
				status = http.StatusForbidden
			case "conflict":
				status = http.StatusConflict
			case "not_found":
				status = http.StatusNotFound
			case "blocked":
				status = http.StatusConflict
			case "unavailable":
				status = http.StatusServiceUnavailable
			case "internal":
				status = http.StatusInternalServerError
			}
			writeError(w, status, commandError.Code, redact(commandError.Message, token))
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Team command failed.")
	}
}
