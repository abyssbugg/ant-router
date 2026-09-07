// Package codexprofile renders isolated Ant Router Codex profiles.
package codexprofile

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/pelletier/go-toml/v2"
)

const ProviderID = "ant-router"

type Options struct {
	BaseURL       string
	CatalogPath   string
	RouterctlPath string
	Instance      string
	StateRoot     string
	RuntimeRoot   string
}

type profile struct {
	ModelProvider    string              `toml:"model_provider"`
	ModelCatalogJSON string              `toml:"model_catalog_json"`
	ModelProviders   map[string]provider `toml:"model_providers"`
}

type provider struct {
	Name              string `toml:"name"`
	BaseURL           string `toml:"base_url"`
	WireAPI           string `toml:"wire_api"`
	RequestMaxRetries int    `toml:"request_max_retries"`
	StreamMaxRetries  int    `toml:"stream_max_retries"`
	Auth              auth   `toml:"auth"`
}

type auth struct {
	Command           string   `toml:"command"`
	Args              []string `toml:"args"`
	TimeoutMS         int      `toml:"timeout_ms"`
	RefreshIntervalMS int      `toml:"refresh_interval_ms"`
}

func Render(options Options) ([]byte, error) {
	if err := validate(options); err != nil {
		return nil, err
	}
	args := []string{"--instance", options.Instance, "--state-dir", options.StateRoot}
	if options.RuntimeRoot != "" {
		args = append(args, "--runtime-dir", options.RuntimeRoot)
	}
	args = append(args, "codex", "token")
	value := profile{ModelProvider: ProviderID, ModelCatalogJSON: options.CatalogPath, ModelProviders: map[string]provider{ProviderID: {Name: "Ant Router", BaseURL: options.BaseURL, WireAPI: "responses", RequestMaxRetries: 0, StreamMaxRetries: 0, Auth: auth{Command: options.RouterctlPath, Args: args, TimeoutMS: 5000, RefreshIntervalMS: 300000}}}}
	encoded, err := toml.Marshal(value)
	if err != nil {
		return nil, errors.New("codex profile encoding failed")
	}
	return encoded, nil
}

func validate(options Options) error {
	parsed, err := url.Parse(options.BaseURL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "/v1" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("invalid Codex profile base URL")
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	portNumber, parseErr := strconv.ParseUint(port, 10, 16)
	if err != nil || host != "127.0.0.1" || parseErr != nil || portNumber == 0 {
		return errors.New("invalid Codex profile base URL")
	}
	if !filepath.IsAbs(options.CatalogPath) || !filepath.IsAbs(options.RouterctlPath) || !filepath.IsAbs(options.StateRoot) || (options.RuntimeRoot != "" && !filepath.IsAbs(options.RuntimeRoot)) {
		return errors.New("invalid Codex profile path")
	}
	if _, err := controltransport.ParseInstanceName(options.Instance); err != nil {
		return errors.New("invalid Codex profile instance")
	}
	return nil
}
