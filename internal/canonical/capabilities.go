package canonical

import "sort"

// Capability identifies a provider-neutral model capability required by a request.
type Capability string

const (
	CapabilityText             Capability = "text"
	CapabilityImageInput       Capability = "image_input"
	CapabilityTools            Capability = "tools"
	CapabilityNamespaceTools   Capability = "namespace_tools"
	CapabilityParallelTools    Capability = "parallel_tools"
	CapabilityReasoningSummary Capability = "reasoning_summary"
	CapabilityStructuredOutput Capability = "structured_output"
	CapabilityContinuation     Capability = "continuation"
)

// CapabilitySet is an immutable sorted set of capabilities.
type CapabilitySet struct{ values []Capability }

// NewCapabilitySet validates, deduplicates, and sorts capabilities.
func NewCapabilitySet(values ...Capability) (CapabilitySet, error) {
	seen := make(map[Capability]struct{}, len(values))
	for _, value := range values {
		if !value.Valid() {
			return CapabilitySet{}, requestError(CodeInvalidInvariant, "capabilities")
		}
		seen[value] = struct{}{}
	}
	result := make([]Capability, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return CapabilitySet{values: result}, nil
}

// Valid reports whether the capability is supported.
func (c Capability) Valid() bool {
	return oneOf(string(c), "text", "image_input", "tools", "namespace_tools", "parallel_tools", "reasoning_summary", "structured_output", "continuation")
}

// Values returns a defensive sorted copy.
func (s CapabilitySet) Values() []Capability { return append([]Capability(nil), s.values...) }

// Has reports whether the set contains capability.
func (s CapabilitySet) Has(capability Capability) bool {
	index := sort.Search(len(s.values), func(index int) bool { return s.values[index] >= capability })
	return index < len(s.values) && s.values[index] == capability
}

// Valid reports whether the set is sorted, unique, and contains valid capabilities.
func (s CapabilitySet) Valid() bool {
	for index, value := range s.values {
		if !value.Valid() || index > 0 && s.values[index-1] >= value {
			return false
		}
	}
	return true
}
