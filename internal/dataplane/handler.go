// Package dataplane exposes authenticated local model APIs over loopback HTTP.
package dataplane

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/openai/responses"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// TokenSource loads the current per-instance inference credential.
type TokenSource interface {
	Token(context.Context) (inferencetoken.Token, error)
}

// TokenSourceFunc adapts a function to TokenSource.
type TokenSourceFunc func(context.Context) (inferencetoken.Token, error)

// Token returns the function result.
func (f TokenSourceFunc) Token(ctx context.Context) (inferencetoken.Token, error) { return f(ctx) }

// StaticTokenSource returns a non-rotating token source, primarily for tests.
func StaticTokenSource(token inferencetoken.Token) TokenSource {
	return TokenSourceFunc(func(context.Context) (inferencetoken.Token, error) { return token, nil })
}

// HandlerOptions configures the authenticated inference handler.
type HandlerOptions struct {
	TokenSource    TokenSource
	Executor       execution.Executor
	Catalog        execution.ModelCatalog
	ListenAddress  string
	MaxConcurrent  int
	RequestTimeout time.Duration
	Clock          func() time.Time
}

type handler struct {
	tokens         TokenSource
	executor       execution.Executor
	catalog        execution.ModelCatalog
	listenAddress  string
	clock          func() time.Time
	requestTimeout time.Duration
	semaphore      chan struct{}
	mux            *http.ServeMux
}

// NewHandler constructs the exact /v1/models and /v1/responses route set.
func NewHandler(options HandlerOptions) (http.Handler, error) {
	if nilInterface(options.TokenSource) || nilInterface(options.Executor) || nilInterface(options.Catalog) || options.ListenAddress == "" || options.MaxConcurrent < 1 || options.MaxConcurrent > 1024 || options.RequestTimeout <= 0 || options.Clock == nil {
		return nil, errors.New("data-plane options are invalid")
	}
	h := &handler{tokens: options.TokenSource, executor: options.Executor, catalog: options.Catalog, listenAddress: options.ListenAddress, clock: options.Clock, requestTimeout: options.RequestTimeout, semaphore: make(chan struct{}, options.MaxConcurrent), mux: http.NewServeMux()}
	h.mux.HandleFunc("/v1/models", h.models)
	h.mux.HandleFunc("/v1/responses", h.responses)
	return http.HandlerFunc(h.serve), nil
}

func (h *handler) serve(w http.ResponseWriter, r *http.Request) {
	tracker := &trackingWriter{ResponseWriter: w}
	defer func() {
		if recover() != nil && !tracker.wrote {
			writeAPIError(tracker, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		}
	}()
	w = tracker
	setSecurityHeaders(w.Header())
	if r.Host != h.listenAddress {
		writeAPIError(w, http.StatusForbidden, "permission_error", "invalid_host", "host", "request host is not allowed")
		return
	}
	token, err := h.tokens.Token(r.Context())
	if err != nil || !validAuthorization(r.Header.Values("Authorization"), token) {
		writeAPIError(w, http.StatusUnauthorized, "authentication_error", "invalid_api_key", "authorization", "authentication failed")
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *handler) models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", "method", "method is not allowed")
		return
	}
	models, err := h.catalog.Models(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "server_error", "model_catalog_unavailable", "model", "model catalog is unavailable")
		return
	}
	type model struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	data := make([]model, 0, len(models))
	for _, value := range models {
		data = append(data, model{ID: value.ID(), Object: "model", Created: h.clock().Unix(), OwnedBy: value.Provider()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (h *handler) responses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", "method", "method is not allowed")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_request_error", "invalid_content_type", "content_type", "content type must be application/json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.requestTimeout)
	defer cancel()
	select {
	case h.semaphore <- struct{}{}:
		defer func() { <-h.semaphore }()
	case <-ctx.Done():
		writeAPIError(w, http.StatusRequestTimeout, "server_error", "request_timeout", "request", "request timed out")
		return
	}
	parsed, err := responses.Decode(r.Body, randomRequestIDs{})
	if err != nil {
		writeResponsesError(w, err)
		return
	}
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		return
	}
	encoder, err := responses.NewEncoder(h.clock())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		return
	}
	var sink streaming.EventSink = discardSink{}
	tracker, ok := w.(*trackingWriter)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		return
	}
	if parsed.Stream {
		if !supportsFlush(w) {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "streaming_unsupported", "stream", "streaming is unavailable")
			return
		}
		flusher := w.(http.Flusher)
		if flusher == nil {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "streaming_unsupported", "stream", "streaming is unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		sink, err = responses.NewSSESink(&httpSSEWriter{writer: tracker, flusher: flusher}, encoder)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "stream", "streaming is unavailable")
			return
		}
	}
	source, err := h.executor.Execute(ctx, parsed.Request)
	if err != nil {
		writeExecutionError(w, err)
		return
	}
	pump, err := streaming.NewPump(machine, source, sink, streaming.DefaultPumpOptions())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "request", "request failed")
		return
	}
	result := pump.Run(ctx)
	if result.Cause() != nil {
		if !tracker.wrote {
			writeAPIError(w, http.StatusBadGateway, "server_error", "upstream_error", "request", "request failed")
		}
		return
	}
	if parsed.Stream {
		return
	}
	response, ok := result.Response()
	if !ok {
		writeAPIError(w, http.StatusBadGateway, "server_error", "upstream_error", "request", "request failed")
		return
	}
	encoded, err := encoder.EncodeResponse(response)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "internal", "response", "response encoding failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	written, writeErr := w.Write(encoded)
	if writeErr != nil || written != len(encoded) {
		return
	}
}

func validAuthorization(values []string, token inferencetoken.Token) bool {
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || strings.Count(values[0], " ") != 1 {
		return false
	}
	return token.Verify(strings.TrimPrefix(values[0], "Bearer "))
}

func writeResponsesError(w http.ResponseWriter, err error) {
	writeAPIError(w, http.StatusBadRequest, "invalid_request_error", string(responses.CodeOf(err)), responses.PathOf(err), "request is invalid")
}
func writeExecutionError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if execution.CodeOf(err) == execution.CodeInvalidModel {
		status = http.StatusBadRequest
	}
	writeAPIError(w, status, "server_error", string(execution.CodeOf(err)), "model", "request could not be executed")
}
func writeAPIError(w http.ResponseWriter, status int, kind, code, param, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"type": kind, "code": code, "param": param, "message": message}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}
func setSecurityHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
}
func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

type randomRequestIDs struct{}

func (randomRequestIDs) RequestID() (canonical.RequestID, error) {
	return canonical.GenerateRequestID(rand.Reader)
}
func (randomRequestIDs) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(rand.Reader)
}
func (randomRequestIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(rand.Reader)
}

type discardSink struct{}

func (discardSink) Send(context.Context, canonical.Event) error { return nil }

type trackingWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *trackingWriter) WriteHeader(status int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}
func (w *trackingWriter) Write(data []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(data)
}
func (w *trackingWriter) Flush() {
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	w.wrote = true
	flusher.Flush()
}
func (w *trackingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func supportsFlush(w http.ResponseWriter) bool {
	if tracker, ok := w.(*trackingWriter); ok {
		_, supported := tracker.ResponseWriter.(http.Flusher)
		return supported
	}
	_, supported := w.(http.Flusher)
	return supported
}

type httpSSEWriter struct {
	writer  *trackingWriter
	flusher http.Flusher
}

func (w *httpSSEWriter) Write(data []byte) (int, error) { return w.writer.Write(data) }
func (w *httpSSEWriter) Flush()                         { w.flusher.Flush() }
