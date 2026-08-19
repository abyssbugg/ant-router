package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
)

type statusProviderFunc func(context.Context) (RuntimeStatus, error)

func (f statusProviderFunc) Status(ctx context.Context) (RuntimeStatus, error) { return f(ctx) }

func newTestRouter(t *testing.T, provider StatusProvider) http.Handler {
	t.Helper()
	router, err := NewRouter(RouterOptions{
		Build:  buildinfo.Info{Version: "1.2.3", Commit: "abc123", Date: "2026-08-19"},
		Status: provider,
		Now:    func() time.Time { return time.Date(2026, 8, 19, 10, 2, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewRouter() failed: %v", err)
	}
	return router
}

func performRequest(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	request = request.WithContext(WithRequestID(request.Context(), "request-123"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestHealthHandler(t *testing.T) {
	recorder := performRequest(t, newTestRouter(t, nil), http.MethodGet, "/v1/health")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response HealthResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if response.Status != "ok" || response.APIVersion != APIVersion {
		t.Errorf("response = %#v", response)
	}
}

func TestVersionHandler(t *testing.T) {
	recorder := performRequest(t, newTestRouter(t, nil), http.MethodGet, "/v1/version")
	var response VersionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if response.Name != "ant-router" || response.Version != "1.2.3" || response.Commit != "abc123" || response.BuiltAt != "2026-08-19" || response.APIVersion != APIVersion {
		t.Errorf("response = %#v", response)
	}
}

func TestStatusHandler(t *testing.T) {
	started := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	provider := statusProviderFunc(func(context.Context) (RuntimeStatus, error) {
		return RuntimeStatus{Instance: "default", DaemonState: "running", StartedAt: started, Config: &ConfigSummary{Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Generation: 4, Status: "active"}}, nil
	})
	recorder := performRequest(t, newTestRouter(t, provider), http.MethodGet, "/v1/status")
	var response StatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if response.Instance != "default" || response.DaemonState != "running" || response.StartedAt != started || response.UptimeSeconds != 120 || response.Config == nil || response.Config.Generation != 4 {
		t.Errorf("response = %#v", response)
	}
}

func TestStatusHandlerOmitsConfigAndClampsUptime(t *testing.T) {
	provider := statusProviderFunc(func(context.Context) (RuntimeStatus, error) {
		return RuntimeStatus{Instance: "dev", DaemonState: "starting", StartedAt: time.Date(2026, 8, 19, 11, 0, 0, 0, time.UTC)}, nil
	})
	recorder := performRequest(t, newTestRouter(t, provider), http.MethodGet, "/v1/status")
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if _, present := response["config"]; present {
		t.Error("config was not omitted")
	}
	if response["uptime_seconds"] != float64(0) {
		t.Errorf("uptime_seconds = %v, want 0", response["uptime_seconds"])
	}
}

func TestStatusProviderErrorAndCancellation(t *testing.T) {
	provider := statusProviderFunc(func(ctx context.Context) (RuntimeStatus, error) {
		if ctx.Err() != nil {
			return RuntimeStatus{}, ctx.Err()
		}
		return RuntimeStatus{}, errors.New("synthetic provider marker")
	})
	router := newTestRouter(t, provider)
	recorder := performRequest(t, router, http.MethodGet, "/v1/status")
	if recorder.Code != http.StatusServiceUnavailable || string(recorder.Body.Bytes()) == "synthetic provider marker" {
		t.Errorf("provider error response = %d %q", recorder.Code, recorder.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil).WithContext(WithRequestID(ctx, "request-123"))
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("cancelled provider status = %d", recorder.Code)
	}
}

func TestRouterMethodsAndUnknownRoutes(t *testing.T) {
	router := newTestRouter(t, nil)
	method := performRequest(t, router, http.MethodPost, "/v1/health")
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodGet {
		t.Errorf("method response = %d headers %#v", method.Code, method.Header())
	}
	missing := performRequest(t, router, http.MethodGet, "/v1/missing")
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing status = %d", missing.Code)
	}
	query := performRequest(t, router, http.MethodGet, "/v1/health?check=true")
	if query.Code != http.StatusOK {
		t.Errorf("query status = %d", query.Code)
	}
}
