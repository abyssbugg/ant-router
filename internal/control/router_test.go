package control

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterDoesNotUseDefaultServeMux(t *testing.T) {
	original := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	t.Cleanup(func() { http.DefaultServeMux = original })
	path := "/synthetic-default-mux-marker"
	http.DefaultServeMux.HandleFunc(path, func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusTeapot)
	})
	router := newTestRouter(t, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("router delegated to DefaultServeMux: status %d", recorder.Code)
	}
}

func TestNewRouterDefaultsClock(t *testing.T) {
	if _, err := NewRouter(RouterOptions{}); err != nil {
		t.Fatalf("NewRouter() failed with default clock: %v", err)
	}
}
