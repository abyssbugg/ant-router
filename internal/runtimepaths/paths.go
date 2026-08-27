package runtimepaths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/abyssbugg/ant-router/internal/controltransport"
)

const (
	EnvironmentInstance   = "ANT_ROUTER_INSTANCE"
	EnvironmentStateDir   = "ANT_ROUTER_STATE_DIR"
	EnvironmentRuntimeDir = "ANT_ROUTER_RUNTIME_DIR"
)

type Options struct {
	InstanceOverride string
	StateOverride    string
	RuntimeOverride  string
	LookupEnv        func(string) string
}

// Paths contains resolved persistent and local-runtime locations.
// RuntimeRoot is intentionally empty on Windows, where named pipes do not use it.
type Paths struct {
	Instance           controltransport.InstanceName
	StateRoot          string
	RuntimeRoot        string
	InstanceDir        string
	TokenPath          string
	InferenceTokenPath string
	ConfigRoot         string
}

func Resolve(options Options) (Paths, error) {
	lookup := options.LookupEnv
	if lookup == nil {
		lookup = os.Getenv
	}
	instanceValue := selectValue(options.InstanceOverride, lookup(EnvironmentInstance), controltransport.DefaultInstance.String())
	instance, err := controltransport.ParseInstanceName(instanceValue)
	if err != nil {
		return Paths{}, err
	}
	stateValue := selectValue(options.StateOverride, lookup(EnvironmentStateDir), "")
	if strings.TrimSpace(stateValue) == "" {
		stateValue, err = defaultStateRoot()
		if err != nil {
			return Paths{}, fmt.Errorf("cannot resolve default state directory: %w", err)
		}
	}
	stateRoot, err := absoluteClean(stateValue)
	if err != nil {
		return Paths{}, fmt.Errorf("cannot resolve state directory: %w", err)
	}
	runtimeValue := selectValue(options.RuntimeOverride, lookup(EnvironmentRuntimeDir), "")
	if strings.TrimSpace(runtimeValue) == "" {
		runtimeValue, err = defaultRuntimeRoot()
		if err != nil {
			return Paths{}, fmt.Errorf("cannot resolve default runtime directory: %w", err)
		}
	}
	runtimeRoot := ""
	if strings.TrimSpace(runtimeValue) != "" {
		runtimeRoot, err = absoluteClean(runtimeValue)
		if err != nil {
			return Paths{}, fmt.Errorf("cannot resolve runtime directory: %w", err)
		}
	}
	instancePaths, err := controltransport.ResolveInstancePaths(stateRoot, instance)
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Instance:           instance,
		StateRoot:          stateRoot,
		RuntimeRoot:        runtimeRoot,
		InstanceDir:        instancePaths.StateDir,
		TokenPath:          instancePaths.TokenPath,
		InferenceTokenPath: filepath.Join(instancePaths.StateDir, "inference.token"),
		ConfigRoot:         filepath.Join(instancePaths.StateDir, "config"),
	}, nil
}

func selectValue(override, environment, fallback string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	if strings.TrimSpace(environment) != "" {
		return environment
	}
	return fallback
}

func absoluteClean(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}
