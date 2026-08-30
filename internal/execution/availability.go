package execution

import "unicode/utf8"

const maxAvailabilityReferenceBytes = 128

// Availability is immutable provider readiness metadata safe for status output.
type Availability struct {
	available bool
	code      ErrorCode
	reference string
}

// Available constructs an available provider status.
func Available() Availability { return Availability{available: true} }

// Unavailable constructs validated unavailable-provider metadata.
func Unavailable(code ErrorCode, reference string) (Availability, error) {
	if code != CodeUnavailable && code != CodeAuthenticationFailed && code != CodePermissionDenied && code != CodeRateLimited && code != CodeInternal {
		return Availability{}, NewError(CodeInternal, "availability.code", 0, nil)
	}
	if !validAvailabilityReference(reference) {
		return Availability{}, NewError(CodeInternal, "availability.reference", 0, nil)
	}
	return Availability{code: code, reference: reference}, nil
}

// Available reports whether the provider can currently accept requests.
func (a Availability) Available() bool { return a.available }

// Code returns the safe reason code when unavailable.
func (a Availability) Code() ErrorCode { return a.code }

// Reference returns optional bounded non-secret diagnostic metadata.
func (a Availability) Reference() string { return a.reference }

// Valid reports whether the availability value satisfies its invariants.
func (a Availability) Valid() bool {
	if a.available {
		return a.code == "" && a.reference == ""
	}
	_, err := Unavailable(a.code, a.reference)
	return err == nil
}

// ProviderStatus associates immutable availability with a provider ID.
type ProviderStatus struct {
	providerID   string
	availability Availability
}

// ProviderID returns the configured provider identifier.
func (s ProviderStatus) ProviderID() string { return s.providerID }

// Availability returns immutable provider readiness metadata.
func (s ProviderStatus) Availability() Availability { return s.availability }

func newProviderStatus(providerID string, availability Availability) (ProviderStatus, error) {
	if !validIdentity(providerID, maxProviderBytes, false) || !availability.Valid() {
		return ProviderStatus{}, NewError(CodeInternal, "provider.status", 0, nil)
	}
	return ProviderStatus{providerID: providerID, availability: availability}, nil
}

func validAvailabilityReference(value string) bool {
	if len(value) > maxAvailabilityReferenceBytes || !utf8.ValidString(value) {
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
