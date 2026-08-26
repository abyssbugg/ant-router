package canonical

import "testing"

func pointer[T any](value T) *T { return &value }

func TestSampling(t *testing.T) {
	sampling, err := NewSampling(SamplingParams{Temperature: pointer(0.7), TopP: pointer(0.9), Seed: pointer[int64](42)})
	if err != nil || !sampling.Valid() {
		t.Fatalf("NewSampling() = %#v, %v", sampling, err)
	}
	if value, ok := sampling.Temperature(); !ok || value != 0.7 {
		t.Errorf("Temperature() = %v, %v", value, ok)
	}
	if value, ok := sampling.TopP(); !ok || value != 0.9 {
		t.Errorf("TopP() = %v, %v", value, ok)
	}
	if value, ok := sampling.Seed(); !ok || value != 42 {
		t.Errorf("Seed() = %v, %v", value, ok)
	}
	zero, err := NewSampling(SamplingParams{})
	if err != nil || !zero.Valid() {
		t.Errorf("zero sampling = %#v, %v", zero, err)
	}
}

func TestSamplingRejectsRanges(t *testing.T) {
	tests := []SamplingParams{
		{Temperature: pointer(-0.1)},
		{Temperature: pointer(2.1)},
		{TopP: pointer(0.0)},
		{TopP: pointer(1.1)},
	}
	for _, params := range tests {
		if _, err := NewSampling(params); codeOfCanonical(err) != CodeInvalidInvariant {
			t.Errorf("params %#v code = %q", params, codeOfCanonical(err))
		}
	}
}
