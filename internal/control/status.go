package control

import (
	"context"
	"errors"
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
	return nil
}

// StatusProvider returns an immutable runtime status and must honor cancellation.
type StatusProvider interface {
	Status(context.Context) (RuntimeStatus, error)
}
