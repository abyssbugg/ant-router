package controltransport

import (
	"errors"
	"path/filepath"
	"strings"
)

// InstanceName identifies an isolated local daemon instance.
type InstanceName string

// DefaultInstance is the conventional single-user daemon instance.
const DefaultInstance InstanceName = "default"

// ParseInstanceName validates a lowercase ASCII instance name.
func ParseInstanceName(value string) (InstanceName, error) {
	if len(value) < 1 || len(value) > 32 || value[0] < 'a' || value[0] > 'z' {
		return "", invalidInstanceError()
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-') {
			return "", invalidInstanceError()
		}
	}
	return InstanceName(value), nil
}

// String returns the validated instance name.
func (n InstanceName) String() string { return string(n) }

// InstancePaths contains persistent per-instance state locations.
type InstancePaths struct {
	Instance  InstanceName
	StateDir  string
	TokenPath string
}

// ResolveInstancePaths derives absolute state and token paths for an instance.
func ResolveInstancePaths(stateRoot string, instance InstanceName) (InstancePaths, error) {
	validated, err := ParseInstanceName(instance.String())
	if err != nil {
		return InstancePaths{}, err
	}
	root := strings.TrimSpace(stateRoot)
	if root == "" {
		return InstancePaths{}, &Error{Code: CodePathResolutionFailed, Path: "state_root", Err: errors.New("state root is required")}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return InstancePaths{}, &Error{Code: CodePathResolutionFailed, Path: "state_root", Err: errors.New("cannot resolve state root")}
	}
	stateDir := filepath.Join(filepath.Clean(absolute), "instances", validated.String())
	return InstancePaths{
		Instance:  validated,
		StateDir:  stateDir,
		TokenPath: filepath.Join(stateDir, "control.token"),
	}, nil
}

func invalidInstanceError() error {
	return &Error{Code: CodeInvalidInstance, Path: "instance", Err: errors.New("instance name is invalid")}
}
