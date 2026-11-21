package workhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func localRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, workcmd.ExecutePath, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:43210"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer session-secret")
	return req
}

const contextBody = `{"name":"context","run_ref":"run-one","payload":{}}`

func TestHandlerRejectsMalformedInputBeforeDispatch(t *testing.T) {
	calls := 0
	handler := NewHandler(workcmd.DispatcherFunc(func(context.Context, string, workcmd.DispatchRequest) (any, error) { calls++; return nil, nil }))
	for name, body := range map[string]string{
		"unknown-envelope":   `{"name":"context","run_ref":"run-one","payload":{},"scope_id":"other"}`,
		"duplicate-envelope": `{"name":"context","name":"task.assign","run_ref":"run-one","payload":{}}`,
		"duplicate-nested":   `{"name":"task.assign","run_ref":"run-one","payload":{"key":"a","key":"b"}}`,
		"multiple-values":    contextBody + ` {}`,
		"missing-run":        `{"name":"context","payload":{}}`,
		"arbitrary-api":      `{"name":"feishu.call","run_ref":"run-one","payload":{"path":"/open-apis/..."}}`,
		"malformed":          `{"name":`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, localRequest(body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid input dispatched %d times", calls)
	}
	oversized := `{"name":"context","run_ref":"run-one","payload":{"large":"` + strings.Repeat("x", workcmd.MaxRequestBytes) + `"}}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, localRequest(oversized))
	if rec.Code != http.StatusRequestEntityTooLarge || calls != 0 {
		t.Fatalf("oversized request status=%d calls=%d", rec.Code, calls)
	}
}

func TestHandlerPeerAuthenticationAndMethodGuards(t *testing.T) {
	calls := 0
	handler := NewHandler(workcmd.DispatcherFunc(func(context.Context, string, workcmd.DispatchRequest) (any, error) { calls++; return nil, nil }))
	for _, test := range []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"remote-peer", func(r *http.Request) { r.RemoteAddr = "192.0.2.1:321"; r.Header.Set("X-Forwarded-For", "127.0.0.1") }, http.StatusForbidden},
		{"missing-token", func(r *http.Request) { r.Header.Del("Authorization") }, http.StatusUnauthorized},
		{"duplicate-auth", func(r *http.Request) { r.Header.Add("Authorization", "Bearer other") }, http.StatusUnauthorized},
		{"get", func(r *http.Request) { r.Method = http.MethodGet }, http.StatusMethodNotAllowed},
		{"wrong-path", func(r *http.Request) { r.URL.Path = "/open-apis/im/v1/messages" }, http.StatusNotFound},
		{"wrong-type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := localRequest(contextBody)
			test.change(req)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != test.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	if calls != 0 {
		t.Fatalf("guarded request dispatched %d times", calls)
	}
}

func TestHandlerPreservesResultAndMapsSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"unauthorized", workauth.ErrUnauthorized, 401, "unauthorized"},
		{"scope", workstore.ErrScopeDenied, 403, "forbidden"},
		{"conflict", workstore.ErrConflict, 409, "conflict"},
		{"missing", workstore.ErrNotFound, 404, "not_found"},
		{"dto", &workcmd.Error{Code: "invalid_argument", Message: "bad field session-secret"}, 400, "invalid_argument"},
		{"storage", errors.New("storage failure session-secret"), 500, "internal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(workcmd.DispatcherFunc(func(_ context.Context, token string, req workcmd.DispatchRequest) (any, error) {
				if token != "session-secret" || req.RunRef != "run-one" {
					t.Error("identity or exact run lost")
				}
				return nil, test.err
			}))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, localRequest(contextBody))
			var envelope response
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if rec.Code != test.status || envelope.Error == nil || envelope.Error.Code != test.code || strings.Contains(rec.Body.String(), "session-secret") {
				t.Fatalf("unsafe response status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	handler := NewHandler(workcmd.DispatcherFunc(func(context.Context, string, workcmd.DispatchRequest) (any, error) {
		return map[string]any{"status": "accepted", "operation_id": "op-one"}, nil
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, localRequest(contextBody))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"accepted"`) || strings.Contains(rec.Body.String(), "completed") {
		t.Fatalf("result rewritten %d %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerUnencodableResultHasNoPartialSuccess(t *testing.T) {
	handler := NewHandler(workcmd.DispatcherFunc(func(context.Context, string, workcmd.DispatchRequest) (any, error) { return make(chan int), nil }))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, localRequest(contextBody))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), `"result"`) {
		t.Fatalf("partial success %d %s", rec.Code, rec.Body.String())
	}
}
