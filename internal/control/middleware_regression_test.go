package control

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestRecoverPanicsDiscardsPartialResponse(t *testing.T) {
	handler := RecoverPanics(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusAccepted)
		_, _ = writer.Write([]byte("synthetic partial marker"))
		panic("synthetic panic marker")
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(WithRequestID(request.Context(), "request-123"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "synthetic partial marker") || strings.Contains(recorder.Body.String(), "synthetic panic marker") {
		t.Errorf("panic response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestRejectBodiesUnknownLength(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Body = io.NopCloser(strings.NewReader("body"))
	request.ContentLength = -1
	request.TransferEncoding = nil
	request = request.WithContext(WithRequestID(request.Context(), "request-123"))
	recorder := httptest.NewRecorder()
	RejectBodies(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("next handler was called")
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestAuthenticateRejectsCommaSeparatedValueBeforeVerification(t *testing.T) {
	calls := 0
	verifier := verifierFunc(func(string) bool { calls++; return true })
	value := strings.Repeat("a", 20) + "," + strings.Repeat("b", 22)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+value)
	recorder := httptest.NewRecorder()
	Authenticate(verifier)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("next handler was called")
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || calls != 0 {
		t.Errorf("status/calls = %d/%d, want 401/0", recorder.Code, calls)
	}
}

func TestRequestIDsSerializesInjectedGenerator(t *testing.T) {
	counter := 0
	generator := func() (string, error) {
		current := counter
		runtime.Gosched()
		counter = current + 1
		return "request-" + strings.Repeat("a", current+1), nil
	}
	handler := RequestIDs(generator)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	const requests = 32
	var wait sync.WaitGroup
	wait.Add(requests)
	for range requests {
		go func() {
			defer wait.Done()
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}()
	}
	wait.Wait()
	if counter != requests {
		t.Errorf("generator counter = %d, want %d", counter, requests)
	}
}

func TestWriteJSONEncodingFailureDoesNotCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	err := WriteJSON(recorder, http.StatusOK, make(chan int))
	if err == nil {
		t.Fatal("WriteJSON() succeeded for unsupported value")
	}
	if recorder.Body.Len() != 0 || recorder.Header().Get("Content-Type") != "" {
		t.Errorf("failed encoding committed response: headers %#v body %q", recorder.Header(), recorder.Body.String())
	}
}

func TestChainOrder(t *testing.T) {
	var order []string
	middleware := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				order = append(order, name+"-before")
				next.ServeHTTP(writer, request)
				order = append(order, name+"-after")
			})
		}
	}
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}), middleware("first"), middleware("second"))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	want := []string{"first-before", "second-before", "handler", "second-after", "first-after"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}
