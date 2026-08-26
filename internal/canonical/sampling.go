package canonical

import "math"

type SamplingParams struct {
	Temperature *float64
	TopP        *float64
	Seed        *int64
}

// Sampling contains optional provider-neutral sampling controls.
type Sampling struct {
	temperature    float64
	hasTemperature bool
	topP           float64
	hasTopP        bool
	seed           int64
	hasSeed        bool
}

// NewSampling validates optional sampling controls.
func NewSampling(params SamplingParams) (Sampling, error) {
	result := Sampling{}
	if params.Temperature != nil {
		if math.IsNaN(*params.Temperature) || math.IsInf(*params.Temperature, 0) || *params.Temperature < 0 || *params.Temperature > 2 {
			return Sampling{}, requestError(CodeInvalidInvariant, "sampling.temperature")
		}
		result.temperature, result.hasTemperature = *params.Temperature, true
	}
	if params.TopP != nil {
		if math.IsNaN(*params.TopP) || math.IsInf(*params.TopP, 0) || *params.TopP <= 0 || *params.TopP > 1 {
			return Sampling{}, requestError(CodeInvalidInvariant, "sampling.top_p")
		}
		result.topP, result.hasTopP = *params.TopP, true
	}
	if params.Seed != nil {
		result.seed, result.hasSeed = *params.Seed, true
	}
	return result, nil
}

func (s Sampling) Temperature() (float64, bool) { return s.temperature, s.hasTemperature }
func (s Sampling) TopP() (float64, bool)        { return s.topP, s.hasTopP }
func (s Sampling) Seed() (int64, bool)          { return s.seed, s.hasSeed }
func (s Sampling) Valid() bool {
	return (!s.hasTemperature || !math.IsNaN(s.temperature) && !math.IsInf(s.temperature, 0) && s.temperature >= 0 && s.temperature <= 2) && (!s.hasTopP || !math.IsNaN(s.topP) && !math.IsInf(s.topP, 0) && s.topP > 0 && s.topP <= 1)
}
