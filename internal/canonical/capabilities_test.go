package canonical

import "testing"

func TestCustomToolsCapability(t *testing.T) {
	set, err := NewCapabilitySet(CapabilityCustomTools)
	if err != nil || !set.Valid() || !set.Has(CapabilityCustomTools) {
		t.Fatalf("custom tools capability = %#v, %v", set, err)
	}
}

func TestCapabilitySet(t *testing.T) {
	set, err := NewCapabilitySet(CapabilityTools, CapabilityText, CapabilityTools, CapabilityImageInput)
	if err != nil || !set.Valid() || !set.Has(CapabilityTools) || set.Has(CapabilityContinuation) {
		t.Fatalf("NewCapabilitySet() = %#v, %v", set, err)
	}
	values := set.Values()
	if len(values) != 3 || values[0] != CapabilityImageInput || values[1] != CapabilityText || values[2] != CapabilityTools {
		t.Errorf("sorted values = %v", values)
	}
	values[0] = CapabilityContinuation
	if set.Values()[0] != CapabilityImageInput {
		t.Error("capability values were mutated")
	}
	if _, err := NewCapabilitySet(Capability("unknown")); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Errorf("invalid capability code = %q", codeOfCanonical(err))
	}
}
