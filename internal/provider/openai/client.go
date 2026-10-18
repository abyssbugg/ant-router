package openai

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/netproxy"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const (
	officialBaseURL      = "https://api.openai.com/v1"
	maxWireResponseBytes = int64(64 << 20)
)

var (
	errRedirectRejected = http.ErrUseLastResponse
	errResponseTooLarge = errors.New("provider response exceeds the wire limit")
)

type clientFactory struct {
	baseURL    string
	httpClient *http.Client
}

func newProductionClientFactory() *clientFactory {
	factory, err := newClientFactory(officialBaseURL, newSecureHTTPClient())
	if err != nil {
		panic("invalid built-in OpenAI client configuration")
	}
	return factory
}

func newClientFactory(baseURL string, client *http.Client) (*clientFactory, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid provider base URL")
	}
	if client == nil || client.Transport == nil {
		return nil, errors.New("invalid provider HTTP client")
	}
	return &clientFactory{baseURL: strings.TrimRight(baseURL, "/"), httpClient: client}, nil
}

func (f *clientFactory) Client(secret credential.Secret) sdk.Client {
	return sdk.NewClient(
		option.WithAPIKey(secret.Reveal()),
		option.WithBaseURL(f.baseURL),
		option.WithHTTPClient(f.httpClient),
		option.WithMaxRetries(0),
	)
}

func (f *clientFactory) BaseURL() string          { return f.baseURL }
func (f *clientFactory) HTTPClient() *http.Client { return f.httpClient }

// newSecureHTTPClient returns the hardened client with direct connections.
func newSecureHTTPClient() *http.Client {
	return newHardenedHTTPClient(nil)
}

// newSecureHTTPClientWithProxy returns the hardened client using the
// configured outbound proxy selection ("" direct, "environment", or a URL).
// Proxying never changes TLS verification of the upstream: HTTPS upstreams
// are tunneled with CONNECT and verified end to end.
func newSecureHTTPClientWithProxy(setting string) (*http.Client, error) {
	proxy, err := netproxy.ParseProxySetting(setting)
	if err != nil {
		return nil, err
	}
	return newHardenedHTTPClient(proxy), nil
}

func newHardenedHTTPClient(proxy netproxy.ProxyFunc) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 proxy,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{
		Transport: &boundedRoundTripper{base: transport, maxBytes: maxWireResponseBytes},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirectRejected
		},
	}
}

type boundedRoundTripper struct {
	base     http.RoundTripper
	maxBytes int64
}

func (t *boundedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	response.Body = &boundedReadCloser{body: response.Body, remaining: t.maxBytes}
	return response, nil
}

type boundedReadCloser struct {
	body      ioReadCloser
	remaining int64
}

type ioReadCloser interface {
	Read([]byte) (int, error)
	Close() error
}

func (r *boundedReadCloser) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if r.remaining > 0 {
		if int64(len(buffer)) > r.remaining {
			buffer = buffer[:r.remaining]
		}
		n, err := r.body.Read(buffer)
		r.remaining -= int64(n)
		if n == 0 && err == nil {
			return 0, io.ErrNoProgress
		}
		return n, err
	}
	var probe [1]byte
	n, err := r.body.Read(probe[:])
	if n > 0 {
		return 0, errResponseTooLarge
	}
	if n == 0 && err == nil {
		return 0, io.ErrNoProgress
	}
	return 0, err
}

func (r *boundedReadCloser) Close() error { return r.body.Close() }
