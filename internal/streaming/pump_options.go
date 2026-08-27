package streaming

import (
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// PumpOptions bounds precommit memory and downstream stalls.
type PumpOptions struct {
	MaxPrecommitEvents int
	MaxPrecommitBytes  int
	StallTimeout       time.Duration
}

// DefaultPumpOptions returns conservative lossless streaming limits.
func DefaultPumpOptions() PumpOptions {
	return PumpOptions{MaxPrecommitEvents: 256, MaxPrecommitBytes: canonical.MaxOrdinaryEventBytes, StallTimeout: 30 * time.Second}
}

// Valid reports whether every pump option is positive and bounded.
func (o PumpOptions) Valid() bool {
	return o.MaxPrecommitEvents > 0 && o.MaxPrecommitBytes > 0 && o.MaxPrecommitBytes <= canonical.MaxOrdinaryEventBytes && o.StallTimeout > 0
}
