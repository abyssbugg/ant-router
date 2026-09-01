package openai

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/credential"
)

func testSecret(t *testing.T, value string) credential.Secret {
	t.Helper()
	source := credential.NewEnvSource(func(name string) (string, bool) { return value, true })
	ref, err := credential.NewRef(credential.SourceEnv, "OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := source.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestSDKClientUsesExplicitCredentialAndZeroRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.URL.Path != "/v1/probe" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer expected-provider-secret" {
			t.Errorf("authorization = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"message":"private upstream message","type":"server_error","code":"server_error"}}`))
	}))
	defer server.Close()
	t.Setenv("OPENAI_API_KEY", "implicit-environment-secret")
	t.Setenv("OPENAI_BASE_URL", "https://attacker.invalid/v1")

	factory, err := newClientFactory(server.URL+"/v1", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := factory.Client(testSecret(t, "expected-provider-secret"))
	var response map[string]any
	err = client.Get(context.Background(), "/probe", nil, &response)
	if err == nil {
		t.Fatal("Get succeeded, want provider error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want exactly 1", calls.Load())
	}
}

func TestProductionClientFactorySecurity(t *testing.T) {
	factory := newProductionClientFactory()
	if factory.BaseURL() != officialBaseURL {
		t.Fatalf("base URL = %q", factory.BaseURL())
	}
	client := factory.HTTPClient()
	if client.Timeout != 0 {
		t.Fatalf("client timeout = %v, want context-owned zero", client.Timeout)
	}
	if err := client.CheckRedirect(nil, nil); !errors.Is(err, errRedirectRejected) {
		t.Fatalf("redirect error = %v", err)
	}
	bounded, ok := client.Transport.(*boundedRoundTripper)
	if !ok {
		t.Fatalf("transport = %T", client.Transport)
	}
	if bounded.maxBytes != maxWireResponseBytes {
		t.Fatalf("wire response bound = %d", bounded.maxBytes)
	}
	transport, ok := bounded.base.(*http.Transport)
	if !ok {
		t.Fatalf("base transport = %T", bounded.base)
	}
	if transport.Proxy != nil || !transport.DisableCompression || !transport.ForceAttemptHTTP2 {
		t.Fatalf("transport security flags = proxy_set:%v compression:%v h2:%v", transport.Proxy != nil, transport.DisableCompression, transport.ForceAttemptHTTP2)
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.MinVersion != tls.VersionTLS12 || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("TLS config = %#v", transport.TLSClientConfig)
	}
	if transport.DialContext == nil || transport.ResponseHeaderTimeout <= 0 || transport.TLSHandshakeTimeout <= 0 || transport.IdleConnTimeout <= 0 {
		t.Fatalf("transport timeouts are not bounded: %#v", transport)
	}
}

func TestSecureHTTPClientRejectsRedirectsAndIgnoresProxyEnvironment(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Store(true) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer origin.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")

	client := newSecureHTTPClient()
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("first-hop request failed: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusFound || redirected.Load() {
		t.Fatalf("status/redirected = %d/%v", response.StatusCode, redirected.Load())
	}
}

func TestSecureHTTPClientBoundsWireResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.CopyN(writer, zeroReader{}, 33)
	}))
	defer server.Close()
	client := server.Client()
	client.Transport = &boundedRoundTripper{base: client.Transport, maxBytes: 32}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, err = io.Copy(io.Discard, response.Body)
	if !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("read error = %v, want response-too-large", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}
