package controltransport

import (
	"errors"
	"path/filepath"
	"strings"
)

// Endpoint describes a local control transport and its token location.
type Endpoint struct {
	Instance  InstanceName
	Network   string
	Address   string
	TokenPath string
}

// NewEndpoint validates an endpoint while preserving its opaque transport address.
func NewEndpoint(instance InstanceName, network, address, tokenPath string) (Endpoint, error) {
	validated, err := ParseInstanceName(instance.String())
	if err != nil {
		return Endpoint{}, err
	}
	network = strings.TrimSpace(network)
	if network == "" {
		return Endpoint{}, invalidEndpointError("network")
	}
	if strings.TrimSpace(address) == "" {
		return Endpoint{}, invalidEndpointError("address")
	}
	tokenPath = strings.TrimSpace(tokenPath)
	if tokenPath == "" {
		return Endpoint{}, invalidEndpointError("token_path")
	}
	absoluteTokenPath, err := filepath.Abs(tokenPath)
	if err != nil {
		return Endpoint{}, &Error{Code: CodePathResolutionFailed, Path: "token_path", Err: errors.New("cannot resolve token path")}
	}
	return Endpoint{
		Instance:  validated,
		Network:   network,
		Address:   address,
		TokenPath: filepath.Clean(absoluteTokenPath),
	}, nil
}

func invalidEndpointError(path string) error {
	return &Error{Code: CodeInvalidEndpoint, Path: path, Err: errors.New("endpoint field is invalid")}
}
