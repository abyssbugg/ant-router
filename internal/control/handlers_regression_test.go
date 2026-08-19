package control

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type failingResponseWriter struct {
	header   http.Header
	statuses []int
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *failingResponseWriter) WriteHeader(status int) { w.statuses = append(w.statuses, status) }
func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestHandlerDoesNotWriteSecondResponseAfterWriteFailure(t *testing.T) {
	router := newTestRouter(t, nil)
	writer := &failingResponseWriter{}
	router.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if len(writer.statuses) != 1 || writer.statuses[0] != http.StatusOK {
		t.Errorf("statuses = %v, want [200]", writer.statuses)
	}
}

func TestStatusProviderInvalidValues(t *testing.T) {
	valid := RuntimeStatus{Instance: "default", DaemonState: "running", StartedAt: time.Now().UTC()}
	tests := []RuntimeStatus{
		{DaemonState: valid.DaemonState, StartedAt: valid.StartedAt},
		{Instance: valid.Instance, StartedAt: valid.StartedAt},
		{Instance: valid.Instance, DaemonState: "unknown", StartedAt: valid.StartedAt},
		{Instance: valid.Instance, DaemonState: valid.DaemonState},
		{Instance: valid.Instance, DaemonState: valid.DaemonState, StartedAt: valid.StartedAt, Config: &ConfigSummary{Revision: "bad", Generation: 1, Status: "active"}},
	}
	for _, status := range tests {
		provider := statusProviderFunc(func(context.Context) (RuntimeStatus, error) { return status, nil })
		recorder := performRequest(t, newTestRouter(t, provider), http.MethodGet, "/v1/status")
		if recorder.Code != http.StatusServiceUnavailable {
			t.Errorf("invalid status %#v returned %d", status, recorder.Code)
		}
	}
}

func TestStatusProviderPanicReturnsUnavailable(t *testing.T) {
	provider := statusProviderFunc(func(context.Context) (RuntimeStatus, error) {
		panic("synthetic provider panic")
	})
	recorder := performRequest(t, newTestRouter(t, provider), http.MethodGet, "/v1/status")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", recorder.Code)
	}
}

func TestBuildStatusResponseCopiesConfig(t *testing.T) {
	config := &ConfigSummary{Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Generation: 1, Status: "active"}
	status := RuntimeStatus{Instance: "default", DaemonState: "running", StartedAt: time.Now().UTC(), Config: config}
	response := buildStatusResponse(status, status.StartedAt.Add(time.Second))
	config.Generation = 99
	if response.Config == nil || response.Config.Generation != 1 {
		t.Errorf("response config was aliased: %#v", response.Config)
	}
}
