package controltransport

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewEndpoint(t *testing.T) {
	tokenPath := filepath.Join("state", "control.token")
	address := `\\.\pipe\ant-router-user-default`
	endpoint, err := NewEndpoint(DefaultInstance, "npipe", address, tokenPath)
	if err != nil {
		t.Fatalf("NewEndpoint() failed: %v", err)
	}
	wantToken, err := filepath.Abs(tokenPath)
	if err != nil {
		t.Fatalf("filepath.Abs() failed: %v", err)
	}
	if endpoint.Instance != DefaultInstance || endpoint.Network != "npipe" || endpoint.Address != address || endpoint.TokenPath != filepath.Clean(wantToken) {
		t.Errorf("endpoint = %#v", endpoint)
	}
}

func TestNewEndpointRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name      string
		instance  InstanceName
		network   string
		address   string
		tokenPath string
	}{
		{name: "instance", instance: InstanceName("../bad"), network: "unix", address: "socket", tokenPath: "token"},
		{name: "network", instance: DefaultInstance, address: "socket", tokenPath: "token"},
		{name: "address", instance: DefaultInstance, network: "unix", tokenPath: "token"},
		{name: "token", instance: DefaultInstance, network: "unix", address: "socket"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewEndpoint(test.instance, test.network, test.address, test.tokenPath)
			if err == nil {
				t.Fatal("NewEndpoint() succeeded for invalid input")
			}
			code, _ := CodeOf(err)
			if test.name == "instance" && code != CodeInvalidInstance {
				t.Errorf("instance code = %q, want %q", code, CodeInvalidInstance)
			}
			if test.name != "instance" && code != CodeInvalidEndpoint {
				t.Errorf("field code = %q, want %q", code, CodeInvalidEndpoint)
			}
		})
	}
}

func TestNewEndpointTrimsNonOpaqueFieldsOnly(t *testing.T) {
	address := `  \\.\pipe\opaque address  `
	endpoint, err := NewEndpoint(DefaultInstance, "  npipe  ", address, "  token  ")
	if err != nil {
		t.Fatalf("NewEndpoint() failed: %v", err)
	}
	if endpoint.Network != "npipe" {
		t.Errorf("Network = %q, want npipe", endpoint.Network)
	}
	if endpoint.Address != address {
		t.Errorf("Address = %q, want opaque %q", endpoint.Address, address)
	}
	if strings.HasPrefix(endpoint.TokenPath, " ") || !filepath.IsAbs(endpoint.TokenPath) {
		t.Errorf("TokenPath = %q, want trimmed absolute path", endpoint.TokenPath)
	}
}
