package control

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"time"

	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/revision"
)

// RuntimeStatus is an immutable status snapshot returned by a StatusProvider.
type RuntimeStatus struct {
	Instance    string
	DaemonState string
	StartedAt   time.Time
	Config      *ConfigSummary
	DataPlane   *DataPlaneSummary
	Providers   []ProviderSummary
}

func (s RuntimeStatus) Validate() error {
	if _, err := controltransport.ParseInstanceName(s.Instance); err != nil {
		return errors.New("runtime status is invalid")
	}
	switch s.DaemonState {
	case "starting", "running", "draining", "stopped":
	default:
		return errors.New("runtime status is invalid")
	}
	if s.StartedAt.IsZero() {
		return errors.New("runtime status is invalid")
	}
	if s.Config != nil {
		if !revision.ID(s.Config.Revision).Valid() || s.Config.Generation == 0 {
			return errors.New("runtime status is invalid")
		}
		switch s.Config.Status {
		case "active", "last_known_good":
		default:
			return errors.New("runtime status is invalid")
		}
	}
	if s.DataPlane != nil && !validDataPlaneSummary(*s.DataPlane) {
		return errors.New("runtime status is invalid")
	}
	if !validProviderSummaries(s.Providers) {
		return errors.New("runtime status is invalid")
	}
	return nil
}

func validDataPlaneSummary(summary DataPlaneSummary) bool {
	if !summary.Enabled {
		return false
	}
	parsed, err := url.Parse(summary.BaseURL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "/v1" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	address, err := netip.ParseAddrPort(parsed.Host)
	return err == nil && address.Addr().Is4() && address.Addr().IsLoopback() && address.Port() != 0
}

func validProviderSummaries(providers []ProviderSummary) bool {
	for index, provider := range providers {
		if !validProviderIdentity(provider.ID) || provider.Kind != "mock" && provider.Kind != "openai" {
			return false
		}
		if index > 0 && providers[index-1].ID >= provider.ID {
			return false
		}
		if provider.Available {
			if provider.Code != "" || provider.Reference != "" {
				return false
			}
			continue
		}
		switch provider.Code {
		case "unavailable", "authentication_failed", "permission_denied", "rate_limited", "internal":
		default:
			return false
		}
		if !validProviderReference(provider.Reference) {
			return false
		}
	}
	return true
}

func validProviderIdentity(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validProviderReference(value string) bool {
	if len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-' || character == '.' || character == ':' {
			continue
		}
		return false
	}
	return true
}

// StatusProvider returns an immutable runtime status and must honor cancellation.
type StatusProvider interface {
	Status(context.Context) (RuntimeStatus, error)
}
