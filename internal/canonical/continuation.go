package canonical

import "unicode/utf8"

// Continuation is bounded opaque provider-affine execution state.
type Continuation struct {
	provider string
	model    string
	data     []byte
}

// NewContinuation validates and copies provider-affine continuation state.
func NewContinuation(provider, model string, data []byte) (Continuation, error) {
	if !validProviderNamespace(provider) {
		return Continuation{}, requestError(CodeContinuationAffinityRequired, "continuation.provider")
	}
	if model == "" {
		return Continuation{}, requestError(CodeContinuationAffinityRequired, "continuation.model")
	}
	if !utf8.ValidString(model) {
		return Continuation{}, requestError(CodeInvalidUTF8, "continuation.model")
	}
	if len(model) > MaxModelNameBytes {
		return Continuation{}, requestError(CodeLimitExceeded, "continuation.model")
	}
	if len(data) == 0 {
		return Continuation{}, requestError(CodeInvalidInvariant, "continuation.data")
	}
	if len(data) > MaxContinuationBytes {
		return Continuation{}, requestError(CodeLimitExceeded, "continuation.data")
	}
	return Continuation{provider: provider, model: model, data: append([]byte(nil), data...)}, nil
}

func (c Continuation) Provider() string { return c.provider }
func (c Continuation) Model() string    { return c.model }
func (c Continuation) Bytes() []byte    { return append([]byte(nil), c.data...) }
func (c Continuation) Valid() bool {
	return validProviderNamespace(c.provider) && c.model != "" && utf8.ValidString(c.model) && len(c.model) <= MaxModelNameBytes && len(c.data) > 0 && len(c.data) <= MaxContinuationBytes
}

func validProviderNamespace(value string) bool {
	if len(value) < 1 || len(value) > MaxProviderNamespaceBytes || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}
