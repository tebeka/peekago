package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:static
var staticFiles embed.FS

type traceFunc func(context.Context, string) (*Trace, error)

const (
	maxBodySize = 64 << 10
	// maxTraces is the number of traces running at once, each one builds and debugs a program.
	maxTraces = 4
)

type server struct {
	trace traceFunc
	slots chan struct{}
}

type traceRequest struct {
	Code string `json:"code"`
}

type traceResponse struct {
	Trace *Trace `json:"trace,omitempty"`
	Error string `json:"error,omitempty"`
}

func newServer(trace traceFunc) *server {
	return &server{trace: trace, slots: make(chan struct{}, maxTraces)}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /trace", s.handleTrace)
	mux.HandleFunc("POST /view", s.handleView)

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}

	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	return mux
}

func (s *server) handleTrace(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req traceRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, traceResponse{Error: "invalid request body"})
		return
	}

	if strings.TrimSpace(req.Code) == "" {
		writeJSON(w, http.StatusBadRequest, traceResponse{Error: "code is required"})
		return
	}

	if !s.acquire() {
		writeJSON(w, http.StatusServiceUnavailable, traceResponse{Error: errBusy.Error()})
		return
	}
	defer s.release()

	trace, err := s.trace(r.Context(), req.Code)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, traceResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, traceResponse{Trace: trace})
}

func (s *server) handleView(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	if strings.TrimSpace(code) == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}

	if !s.acquire() {
		http.Error(w, errBusy.Error(), http.StatusServiceUnavailable)
		return
	}
	defer s.release()

	trace, err := s.trace(r.Context(), code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := RenderHTML(w, traceFileName, trace); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var errBusy = errors.New("server busy, try again later")

func (s *server) acquire() bool {
	select {
	case s.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *server) release() {
	<-s.slots
}

func decodeJSON(r io.Reader, out any) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}

		return err
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}
