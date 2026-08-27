package inferencetoken

import (
	"strings"
	"testing"
)

func TestTemporaryPatternsAreInferenceSpecific(t *testing.T) {
	for name, pattern := range map[string]string{
		"create": createTemporaryPattern,
		"rotate": rotateTemporaryPattern,
	} {
		if !strings.HasPrefix(pattern, ".inference.token.") || strings.Contains(pattern, "control") {
			t.Errorf("%s temporary pattern is not inference-specific", name)
		}
	}
}
