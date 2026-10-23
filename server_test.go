package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestServerRoutesIndex(t *testing.T) {
	srv := newServer(func(context.Context, string) (*Trace, error) {
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if !strings.Contains(rec.Body.String(), "Peek-A-Go") {
		t.Fatalf("body %q does not contain project name", rec.Body.String())
	}
}

func TestServerView(t *testing.T) {
	srv := newServer(func(context.Context, string) (*Trace, error) {
		return &Trace{Source: "package main", Steps: []Step{{File: "main.go", Line: 1}}}, nil
	})

	form := url.Values{"code": {"package main"}}
	req := httptest.NewRequest(http.MethodPost, "/view", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if !strings.Contains(rec.Body.String(), "Peek-A-Go — main.go") {
		t.Fatalf("body %q does not contain viewer title", rec.Body.String())
	}
}

func TestServerViewEmptyCode(t *testing.T) {
	srv := newServer(func(context.Context, string) (*Trace, error) {
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/view", strings.NewReader("code=+"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestServerTraceSuccess(t *testing.T) {
	srv := newServer(func(context.Context, string) (*Trace, error) {
		return &Trace{Steps: []Step{{File: "main.go", Line: 3}}}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader(`{"code":"package main"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp traceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if resp.Trace == nil || len(resp.Trace.Steps) != 1 || resp.Trace.Steps[0].Line != 3 {
		t.Fatalf("trace = %#v, want one step at line 3", resp.Trace)
	}
}

func TestServerTraceErrors(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		trace  traceFunc
		status int
		errMsg string
	}{
		{
			name:   "bad json",
			body:   `{"code":`,
			trace:  func(context.Context, string) (*Trace, error) { return nil, nil },
			status: http.StatusBadRequest,
			errMsg: "invalid request body",
		},
		{
			name:   "empty code",
			body:   `{"code":"   "}`,
			trace:  func(context.Context, string) (*Trace, error) { return nil, nil },
			status: http.StatusBadRequest,
			errMsg: "code is required",
		},
		{
			name: "trace failure",
			body: `{"code":"package main"}`,
			trace: func(context.Context, string) (*Trace, error) {
				return nil, errors.New("boom")
			},
			status: http.StatusBadRequest,
			errMsg: "boom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newServer(tc.trace)
			req := httptest.NewRequest(http.MethodPost, "/trace", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			srv.routes().ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}

			var resp traceResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			if resp.Error != tc.errMsg {
				t.Fatalf("error = %q, want %q", resp.Error, tc.errMsg)
			}
		})
	}
}
