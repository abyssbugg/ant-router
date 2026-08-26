package canonicaljson

// MaxDefaultBytes is the default maximum encoded JSON size.
const MaxDefaultBytes = 1 << 20

// Limits bounds input bytes and nested container depth. A root container has depth one.
type Limits struct {
	MaximumBytes int
	MaximumDepth int
}

// DefaultLimits returns the Phase 2A 1 MiB and depth-64 limits.
func DefaultLimits() Limits {
	return Limits{MaximumBytes: MaxDefaultBytes, MaximumDepth: 64}
}
