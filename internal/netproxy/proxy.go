package netproxy

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Environment selects proxies from HTTP_PROXY, HTTPS_PROXY, and NO_PROXY.
const Environment = "environment"

// ProxyFunc is a function signature for http.Transport.Proxy.
// It returns the proxy URL for a given request, or nil for direct connection.
type ProxyFunc func(*http.Request) (*url.URL, error)

// ParseProxySetting parses a proxy configuration string and returns a ProxyFunc
// that can be used with http.Transport.Proxy.
//
// Valid inputs:
//   - "" (empty): returns nil (direct connection)
//   - "environment": returns http.ProxyFromEnvironment (honors HTTP_PROXY/HTTPS_PROXY/NO_PROXY)
//   - "http://...", "https://...", "socks5://...": returns a proxy URL function for that URL
//
// Rejects:
//   - URLs with userinfo (user:password)
//   - Unsupported schemes
//   - URLs with empty host
//   - URLs with path, query, or fragment
func ParseProxySetting(setting string) (ProxyFunc, error) {
	setting = strings.TrimSpace(setting)

	// Empty string means direct connection.
	if setting == "" {
		return nil, nil
	}

	// Special case: "environment" honors env vars via http.ProxyFromEnvironment.
	if setting == Environment {
		return http.ProxyFromEnvironment, nil
	}

	// Parse as URL.
	parsed, err := url.Parse(setting)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	// Reject userinfo.
	if parsed.User != nil {
		return nil, errors.New("proxy URL must not contain credentials (user:password)")
	}

	// Reject path, query, fragment.
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("proxy URL must not contain path, query, or fragment")
	}

	// Reject empty or missing scheme and host.
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("proxy URL must have scheme and host")
	}

	// Allow only http, https, socks5.
	switch parsed.Scheme {
	case "http", "https", "socks5":
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %q", parsed.Scheme)
	}

	// Return a function that always returns this proxy URL.
	return func(*http.Request) (*url.URL, error) {
		return parsed, nil
	}, nil
}

// ValidateProxySetting validates a proxy configuration string without returning a ProxyFunc.
// It returns a non-nil error if the setting is invalid.
func ValidateProxySetting(setting string) error {
	_, err := ParseProxySetting(setting)
	return err
}
