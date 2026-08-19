package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/configservice"
)

type configServiceFake struct {
	validateResult configservice.ValidationResult
	validateErr    error
	applyResult    configservice.CurrentResult
	applyErr       error
	currentResult  configservice.CurrentResult
	rollbackResult configservice.CurrentResult
	rollbackErr    error
	validatedTOML  []byte
	applyRequest   configservice.ApplyRequest
}

func (f *configServiceFake) Validate(ctx context.Context, data []byte) (configservice.ValidationResult, error) {
	f.validatedTOML = append([]byte(nil), data...)
	return f.validateResult, f.validateErr
}
func (f *configServiceFake) Apply(ctx context.Context, request configservice.ApplyRequest) (configservice.CurrentResult, error) {
	f.applyRequest = request
	return f.applyResult, f.applyErr
}
func (f *configServiceFake) Current() configservice.CurrentResult { return f.currentResult }
func (f *configServiceFake) Rollback(ctx context.Context) (configservice.CurrentResult, error) {
	return f.rollbackResult, f.rollbackErr
}

func configRouter(t *testing.T, service ConfigService) http.Handler {
	t.Helper()
	router, err := NewRouter(RouterOptions{Config: service})
	if err != nil {
		t.Fatalf("NewRouter() failed: %v", err)
	}
	return RequestPolicy(router)
}

func configRequest(t *testing.T, handler http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(WithRequestID(context.Background(), "request-123"), method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestConfigCurrentHandler(t *testing.T) {
	service := &configServiceFake{currentResult: configservice.CurrentResult{State: configservice.StateAwaitingConfiguration}}
	recorder := configRequest(t, configRouter(t, service), http.MethodGet, "/v1/config", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response ConfigCurrentResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if response.State != "awaiting_configuration" || response.Revision != "" {
		t.Errorf("response = %#v", response)
	}
}

func TestConfigValidateHandler(t *testing.T) {
	revision := config.RevisionID(strings.Repeat("a", 64))
	service := &configServiceFake{validateResult: configservice.ValidationResult{Revision: revision, BaseGeneration: 4, CanonicalSize: 142}}
	body := `{"toml":"schema_version = 1\n"}`
	recorder := configRequest(t, configRouter(t, service), http.MethodPost, "/v1/config/validate", body, "application/json")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %q", recorder.Code, recorder.Body.String())
	}
	if string(service.validatedTOML) != "schema_version = 1\n" {
		t.Errorf("validated TOML = %q", service.validatedTOML)
	}
	var response ConfigValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if response.Revision != revision.String() || response.BaseGeneration != 4 || response.CanonicalSize != 142 {
		t.Errorf("response = %#v", response)
	}
}

func TestConfigApplyAndRollbackHandlers(t *testing.T) {
	revision := strings.Repeat("b", 64)
	result := configservice.CurrentResult{State: configservice.StateRunning, Revision: config.RevisionID(revision), Generation: 5, Status: config.SnapshotStatusActive, ActivatedAt: time.Date(2026, 8, 19, 18, 0, 0, 0, time.UTC)}
	service := &configServiceFake{applyResult: result, rollbackResult: result}
	applyBody := `{"toml":"schema_version = 1\n","expected_revision":"` + revision + `","expected_generation":4}`
	apply := configRequest(t, configRouter(t, service), http.MethodPost, "/v1/config/apply", applyBody, "application/json")
	if apply.Code != http.StatusOK {
		t.Fatalf("apply status = %d body %q", apply.Code, apply.Body.String())
	}
	if service.applyRequest.ExpectedRevision.String() != revision || service.applyRequest.ExpectedGeneration != 4 {
		t.Errorf("apply request = %#v", service.applyRequest)
	}
	rollback := configRequest(t, configRouter(t, service), http.MethodPost, "/v1/config/rollback", "", "")
	if rollback.Code != http.StatusOK {
		t.Fatalf("rollback status = %d", rollback.Code)
	}
}

func TestConfigHandlerErrorMapping(t *testing.T) {
	tests := []struct {
		code configservice.Code
		want int
	}{
		{configservice.CodeConfigurationInvalid, http.StatusBadRequest},
		{configservice.CodeConfigurationTooLarge, http.StatusRequestEntityTooLarge},
		{configservice.CodeRevisionMismatch, http.StatusConflict},
		{configservice.CodeGenerationConflict, http.StatusConflict},
		{configservice.CodeNoConfiguration, http.StatusNotFound},
		{configservice.CodeNoPreviousRevision, http.StatusNotFound},
		{configservice.CodeConfigurationUnavailable, http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		service := &configServiceFake{validateErr: &configservice.Error{Code: test.code, Err: errors.New("synthetic internal marker")}}
		recorder := configRequest(t, configRouter(t, service), http.MethodPost, "/v1/config/validate", `{"toml":"schema_version = 1\n"}`, "application/json")
		if recorder.Code != test.want || strings.Contains(recorder.Body.String(), "synthetic internal marker") {
			t.Errorf("code %q response = %d %q", test.code, recorder.Code, recorder.Body.String())
		}
	}
}

func TestConfigHandlersRejectMalformedRequests(t *testing.T) {
	service := &configServiceFake{}
	handler := configRouter(t, service)
	tests := []struct {
		name        string
		path        string
		body        string
		contentType string
		want        int
	}{
		{name: "content type", path: "/v1/config/validate", body: `{}`, contentType: "text/plain", want: http.StatusUnsupportedMediaType},
		{name: "unknown field", path: "/v1/config/validate", body: `{"toml":"x","extra":true}`, contentType: "application/json", want: http.StatusBadRequest},
		{name: "trailing", path: "/v1/config/validate", body: `{"toml":"x"} {}`, contentType: "application/json", want: http.StatusBadRequest},
		{name: "oversized", path: "/v1/config/validate", body: strings.Repeat("x", int(MaxConfigRequestBody)+1), contentType: "application/json", want: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		recorder := configRequest(t, handler, http.MethodPost, test.path, test.body, test.contentType)
		if recorder.Code != test.want {
			t.Errorf("%s status = %d, want %d", test.name, recorder.Code, test.want)
		}
	}
}
