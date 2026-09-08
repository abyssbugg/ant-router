package control

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
	"github.com/abyssbugg/ant-router/internal/codexcatalog"
	"github.com/abyssbugg/ant-router/internal/config"
)

type codexCatalogProviderFunc func(context.Context) (CodexCatalog, error)

func (f codexCatalogProviderFunc) CodexCatalog(ctx context.Context) (CodexCatalog, error) {
	return f(ctx)
}

func testCodexCatalogBytes(t *testing.T) []byte {
	t.Helper()
	encoded, err := codexcatalog.Generate([]codexcatalog.Projection{{Alias: "mock/text", Capabilities: []string{"text"}, Metadata: config.CodexModelConfig{Enabled: true, DisplayName: "Mock Text", Description: "Deterministic text model", ContextWindow: 32768, EffectiveContextWindowPercent: 90, Priority: 10, Visibility: config.CodexVisibilityList}}})
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	return encoded
}

func TestCodexCatalogRouteAndMethod(t *testing.T) {
	catalog := testCodexCatalogBytes(t)
	provider := codexCatalogProviderFunc(func(context.Context) (CodexCatalog, error) { return CodexCatalog{Generation: 7, JSON: catalog}, nil })
	router, err := NewRouter(RouterOptions{Build: buildinfo.Info{Version: "test"}, CodexCatalog: provider, Now: func() time.Time { return time.Unix(1, 0).UTC() }})
	if err != nil {
		t.Fatalf("NewRouter() failed: %v", err)
	}
	response := performRequest(t, router, http.MethodGet, "/v1/codex/catalog")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	var decoded CodexCatalogResponse
	if err := strictDecodeJSON(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.APIVersion != APIVersion || decoded.Generation != 7 || decoded.Catalog != string(catalog) {
		t.Fatalf("response = %#v", decoded)
	}
	method := performRequest(t, router, http.MethodPost, "/v1/codex/catalog")
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST status = %d, Allow = %q", method.Code, method.Header().Get("Allow"))
	}
}

func TestCodexCatalogProviderFailuresAreUnavailable(t *testing.T) {
	providers := []CodexCatalogProvider{
		codexCatalogProviderFunc(func(context.Context) (CodexCatalog, error) { panic("private marker") }),
		codexCatalogProviderFunc(func(context.Context) (CodexCatalog, error) { return CodexCatalog{}, nil }),
	}
	for _, provider := range providers {
		router, err := NewRouter(RouterOptions{Build: buildinfo.Info{Version: "test"}, CodexCatalog: provider})
		if err != nil {
			t.Fatal(err)
		}
		response := performRequest(t, router, http.MethodGet, "/v1/codex/catalog")
		if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private marker") {
			t.Fatalf("response = %d %q", response.Code, response.Body.String())
		}
	}
}

func TestCodexCatalogResponseValidationRejectsCorruption(t *testing.T) {
	catalog, err := NewCodexCatalog(3, testCodexCatalogBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	valid := catalog.response()
	cases := []CodexCatalogResponse{valid, valid, valid, valid}
	cases[0].Generation = 0
	cases[1].Catalog = "not-json"
	cases[2].SHA256 = strings.Repeat("0", 64)
	cases[3].Catalog = `{"padding":"` + strings.Repeat("a", MaxCodexCatalogBytes) + `"}`
	for index, candidate := range cases {
		if err := validateCodexCatalogResponse(candidate); err == nil {
			t.Errorf("case %d accepted", index)
		}
	}
}

func TestClientCodexCatalog(t *testing.T) {
	catalog := testCodexCatalogBytes(t)
	provider := codexCatalogProviderFunc(func(context.Context) (CodexCatalog, error) { return CodexCatalog{Generation: 9, JSON: catalog}, nil })
	router, err := NewRouter(RouterOptions{Build: buildinfo.Info{Version: "test"}, CodexCatalog: provider})
	if err != nil {
		t.Fatalf("NewRouter() failed: %v", err)
	}
	client, _, _ := testClient(t, router)
	response, err := client.CodexCatalog(context.Background())
	if err != nil {
		t.Fatalf("CodexCatalog() failed: %v", err)
	}
	if response.Generation != 9 || response.Catalog != string(catalog) {
		t.Fatalf("response = %#v", response)
	}
}
