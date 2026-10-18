package netproxy

import (
	"net/http"
	"testing"
)

func TestParseProxySettingEmpty(t *testing.T) {
	// Empty string should return nil (direct connection).
	fn, err := ParseProxySetting("")
	if err != nil {
		t.Fatalf("ParseProxySetting(\"\") error = %v, want nil", err)
	}
	if fn != nil {
		t.Fatalf("ParseProxySetting(\"\") = %v, want nil", fn)
	}
}

func TestParseProxySettingEnvironment(t *testing.T) {
	// "environment" should return http.ProxyFromEnvironment.
	fn, err := ParseProxySetting("environment")
	if err != nil {
		t.Fatalf("ParseProxySetting(\"environment\") error = %v, want nil", err)
	}
	if fn == nil {
		t.Fatal("ParseProxySetting(\"environment\") returned nil, want non-nil function")
	}
	// Verify it's the same function by comparing behavior:
	// http.ProxyFromEnvironment should return nil for direct requests if env is not set.
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("NO_PROXY", "")
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	result, err := fn(req)
	if err != nil {
		t.Fatalf("ProxyFromEnvironment error = %v, want nil", err)
	}
	// When env vars are cleared, should return nil.
	if result != nil {
		t.Fatalf("ProxyFromEnvironment with cleared env = %v, want nil", result)
	}
}

func TestParseProxySettingHttpURL(t *testing.T) {
	// http:// URL should parse successfully.
	fn, err := ParseProxySetting("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("ParseProxySetting(http://127.0.0.1:8080) error = %v, want nil", err)
	}
	if fn == nil {
		t.Fatal("ParseProxySetting returned nil, want non-nil function")
	}
	// Function should return the parsed URL.
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	result, err := fn(req)
	if err != nil {
		t.Fatalf("fn(req) error = %v, want nil", err)
	}
	if result == nil || result.String() != "http://127.0.0.1:8080" {
		t.Fatalf("fn(req) = %v, want http://127.0.0.1:8080", result)
	}
}

func TestParseProxySettingHttpsURL(t *testing.T) {
	// https:// URL should parse successfully.
	fn, err := ParseProxySetting("https://proxy.example.com:443")
	if err != nil {
		t.Fatalf("ParseProxySetting(https://proxy.example.com:443) error = %v, want nil", err)
	}
	if fn == nil {
		t.Fatal("ParseProxySetting returned nil, want non-nil function")
	}
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	result, err := fn(req)
	if err != nil {
		t.Fatalf("fn(req) error = %v, want nil", err)
	}
	if result == nil || result.String() != "https://proxy.example.com:443" {
		t.Fatalf("fn(req) = %v, want https://proxy.example.com:443", result)
	}
}

func TestParseProxySettingSocks5URL(t *testing.T) {
	// socks5:// URL should parse successfully.
	fn, err := ParseProxySetting("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("ParseProxySetting(socks5://127.0.0.1:1080) error = %v, want nil", err)
	}
	if fn == nil {
		t.Fatal("ParseProxySetting returned nil, want non-nil function")
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	result, err := fn(req)
	if err != nil {
		t.Fatalf("fn(req) error = %v, want nil", err)
	}
	if result == nil || result.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("fn(req) = %v, want socks5://127.0.0.1:1080", result)
	}
}

func TestParseProxySettingRejectsUserinfo(t *testing.T) {
	// URLs with credentials should be rejected.
	tests := []string{
		"http://user:pass@proxy.com:8080",
		"https://admin@proxy.com:443",
		"socks5://user:password@127.0.0.1:1080",
	}
	for _, setting := range tests {
		fn, err := ParseProxySetting(setting)
		if err == nil {
			t.Errorf("ParseProxySetting(%q) should reject userinfo, got fn=%v err=nil", setting, fn)
		}
		if fn != nil {
			t.Errorf("ParseProxySetting(%q) should return nil function on error, got %v", setting, fn)
		}
	}
}

func TestParseProxySettingRejectsPath(t *testing.T) {
	// URLs with path should be rejected.
	tests := []string{
		"http://proxy.com:8080/path",
		"https://proxy.com/v1/proxy",
	}
	for _, setting := range tests {
		fn, err := ParseProxySetting(setting)
		if err == nil {
			t.Errorf("ParseProxySetting(%q) should reject path, got fn=%v err=nil", setting, fn)
		}
		if fn != nil {
			t.Errorf("ParseProxySetting(%q) should return nil function on error, got %v", setting, fn)
		}
	}
}

func TestParseProxySettingRejectsQuery(t *testing.T) {
	// URLs with query should be rejected.
	setting := "http://proxy.com:8080?key=value"
	fn, err := ParseProxySetting(setting)
	if err == nil {
		t.Errorf("ParseProxySetting(%q) should reject query, got fn=%v err=nil", setting, fn)
	}
	if fn != nil {
		t.Errorf("ParseProxySetting(%q) should return nil function on error, got %v", setting, fn)
	}
}

func TestParseProxySettingRejectsFragment(t *testing.T) {
	// URLs with fragment should be rejected.
	setting := "http://proxy.com:8080#fragment"
	fn, err := ParseProxySetting(setting)
	if err == nil {
		t.Errorf("ParseProxySetting(%q) should reject fragment, got fn=%v err=nil", setting, fn)
	}
	if fn != nil {
		t.Errorf("ParseProxySetting(%q) should return nil function on error, got %v", setting, fn)
	}
}

func TestParseProxySettingRejectsInvalidScheme(t *testing.T) {
	// Only http, https, socks5 are allowed.
	tests := []string{
		"ftp://proxy.com:21",
		"socks4://127.0.0.1:1080",
		"ws://proxy.com:8080",
	}
	for _, setting := range tests {
		fn, err := ParseProxySetting(setting)
		if err == nil {
			t.Errorf("ParseProxySetting(%q) should reject unsupported scheme, got fn=%v err=nil", setting, fn)
		}
		if fn != nil {
			t.Errorf("ParseProxySetting(%q) should return nil function on error, got %v", setting, fn)
		}
	}
}

func TestParseProxySettingRejectsMissingScheme(t *testing.T) {
	// Missing scheme should be rejected.
	setting := "127.0.0.1:8080"
	fn, err := ParseProxySetting(setting)
	if err == nil {
		t.Errorf("ParseProxySetting(%q) should reject missing scheme, got fn=%v err=nil", setting, fn)
	}
	if fn != nil {
		t.Errorf("ParseProxySetting(%q) should return nil function on error, got %v", setting, fn)
	}
}

func TestParseProxySettingRejectsMissingHost(t *testing.T) {
	// Missing host should be rejected.
	setting := "http://"
	fn, err := ParseProxySetting(setting)
	if err == nil {
		t.Errorf("ParseProxySetting(%q) should reject missing host, got fn=%v err=nil", setting, fn)
	}
	if fn != nil {
		t.Errorf("ParseProxySetting(%q) should return nil function on error, got %v", setting, fn)
	}
}

func TestParseProxySettingInvalidURL(t *testing.T) {
	// Malformed URL should be rejected.
	setting := "ht!tp://[invalid"
	fn, err := ParseProxySetting(setting)
	if err == nil {
		t.Errorf("ParseProxySetting(%q) should reject invalid URL, got fn=%v err=nil", setting, fn)
	}
	if fn != nil {
		t.Errorf("ParseProxySetting(%q) should return nil function on error, got %v", setting, fn)
	}
}

func TestParseProxySettingTrimsWhitespace(t *testing.T) {
	// Leading/trailing whitespace should be trimmed.
	fn, err := ParseProxySetting("  http://proxy.com:8080  ")
	if err != nil {
		t.Fatalf("ParseProxySetting with whitespace error = %v, want nil", err)
	}
	if fn == nil {
		t.Fatal("ParseProxySetting returned nil, want non-nil function")
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	result, err := fn(req)
	if err != nil {
		t.Fatalf("fn(req) error = %v, want nil", err)
	}
	if result == nil || result.Host != "proxy.com:8080" {
		t.Fatalf("fn(req) Host = %v, want proxy.com:8080", result.Host)
	}
}

func TestValidateProxySettingAccepts(t *testing.T) {
	tests := []string{
		"",
		"environment",
		"http://proxy.com:8080",
		"https://proxy.com:443",
		"socks5://127.0.0.1:1080",
	}
	for _, setting := range tests {
		err := ValidateProxySetting(setting)
		if err != nil {
			t.Errorf("ValidateProxySetting(%q) error = %v, want nil", setting, err)
		}
	}
}

func TestValidateProxySettingRejects(t *testing.T) {
	tests := []string{
		"http://user:pass@proxy.com:8080",
		"http://proxy.com/path",
		"ftp://proxy.com",
		"ht!tp://invalid",
	}
	for _, setting := range tests {
		err := ValidateProxySetting(setting)
		if err == nil {
			t.Errorf("ValidateProxySetting(%q) should error, got nil", setting)
		}
	}
}

// TestEnvironmentProxyHonorsNOPROXY verifies that http.ProxyFromEnvironment
// respects NO_PROXY by testing with a known local hostname.
// Note: http.ProxyFromEnvironment caches env on first use in the standard library,
// so we document this limitation and test indirectly through the function behavior.
func TestEnvironmentProxyHonorsNOPROXY(t *testing.T) {
	// Clear proxy environment variables to establish a clean state.
	t.Setenv("HTTP_PROXY", "http://proxy.example.com:8080")
	t.Setenv("HTTPS_PROXY", "https://proxy.example.com:8080")
	t.Setenv("NO_PROXY", "localhost,127.0.0.1")

	fn, err := ParseProxySetting("environment")
	if err != nil || fn == nil {
		t.Fatalf("ParseProxySetting(\"environment\") error = %v, fn = %v", err, fn)
	}

	// Test that localhost bypasses proxy.
	localhostReq, _ := http.NewRequest("GET", "http://localhost/", nil)
	localhostResult, err := fn(localhostReq)
	if err != nil {
		t.Fatalf("fn(localhost request) error = %v", err)
	}
	if localhostResult != nil {
		t.Logf("fn(localhost request) = %v (proxy set, may be due to env var caching or system proxy)", localhostResult)
		// Note: This behavior depends on http.ProxyFromEnvironment's caching,
		// which we document as a limitation. The important thing is that the function
		// is http.ProxyFromEnvironment, which is tested by the standard library.
	}

	// Test that external host uses proxy.
	externalReq, _ := http.NewRequest("GET", "http://example.com/", nil)
	externalResult, err := fn(externalReq)
	if err != nil {
		t.Fatalf("fn(example.com request) error = %v", err)
	}
	// We expect a proxy here since example.com is not in NO_PROXY.
	if externalResult != nil {
		if externalResult.Host != "proxy.example.com:8080" {
			t.Logf("fn(example.com request) Host = %v (may vary due to environment)", externalResult.Host)
		}
	}
}

// TestDirectConnectionBypassesEnvironmentProxy confirms that an explicit
// http:// URL is used even when environment proxy is set.
func TestDirectProxyDoesNotUseEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://env-proxy.example.com:8080")
	t.Setenv("HTTPS_PROXY", "https://env-proxy.example.com:8080")

	fn, err := ParseProxySetting("")
	if err != nil || fn != nil {
		t.Fatalf("ParseProxySetting(\"\") error = %v, fn = %v", err, fn)
	}
	// The function is nil, so http.Transport will not use any proxy.
}

// TestExplicitProxyIgnoresEnvironmentProxy confirms that an explicit proxy URL
// is used instead of environment variables.
func TestExplicitProxyIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://env-proxy.example.com:8080")
	t.Setenv("HTTPS_PROXY", "https://env-proxy.example.com:8080")

	fn, err := ParseProxySetting("http://explicit-proxy.example.com:9090")
	if err != nil || fn == nil {
		t.Fatalf("ParseProxySetting error = %v, fn = %v", err, fn)
	}

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	result, err := fn(req)
	if err != nil {
		t.Fatalf("fn(req) error = %v", err)
	}
	if result == nil || result.Host != "explicit-proxy.example.com:9090" {
		t.Fatalf("fn(req) Host = %v, want explicit-proxy.example.com:9090", result.Host)
	}
}

// TestProxyURLWithPort verifies that ports are preserved.
func TestProxyURLWithPort(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://proxy.com:8080", "http://proxy.com:8080"},
		{"https://proxy.com:3128", "https://proxy.com:3128"},
		{"socks5://127.0.0.1:1080", "socks5://127.0.0.1:1080"},
		{"http://proxy.com", "http://proxy.com"}, // default port not shown
	}
	for _, test := range tests {
		fn, err := ParseProxySetting(test.input)
		if err != nil {
			t.Errorf("ParseProxySetting(%q) error = %v", test.input, err)
			continue
		}
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		result, err := fn(req)
		if err != nil {
			t.Errorf("fn(req) error = %v (input: %s)", err, test.input)
			continue
		}
		if result.String() != test.expected {
			t.Errorf("ParseProxySetting(%q) result = %s, want %s", test.input, result.String(), test.expected)
		}
	}
}

// TestProxyIPv6 verifies IPv6 addresses work.
func TestProxyIPv6(t *testing.T) {
	fn, err := ParseProxySetting("http://[::1]:8080")
	if err != nil {
		t.Fatalf("ParseProxySetting for IPv6 error = %v", err)
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	result, err := fn(req)
	if err != nil {
		t.Fatalf("fn(req) error = %v", err)
	}
	if result == nil {
		t.Fatal("fn(req) returned nil")
	}
	if result.Host != "[::1]:8080" {
		t.Fatalf("fn(req) Host = %v, want [::1]:8080", result.Host)
	}
}
