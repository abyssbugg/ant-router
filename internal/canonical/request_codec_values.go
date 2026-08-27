package canonical

import (
	"encoding/json"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func encodeTool(value ToolDefinition) toolDTO {
	return toolDTO{Name: value.Name(), Description: value.Description(), InputSchema: json.RawMessage(value.InputSchema().Bytes()), SchemaEnforcement: string(value.Enforcement())}
}

func decodeTool(dto toolDTO) (ToolDefinition, error) {
	schema, err := canonicaljson.Parse(dto.InputSchema, canonicaljson.Limits{MaximumBytes: MaxToolSchemaBytes, MaximumDepth: MaxJSONDepth})
	if err != nil {
		return ToolDefinition{}, codecError(CodeInvalidJSON, "tool.input_schema")
	}
	return NewToolDefinitionWithEnforcement(dto.Name, dto.Description, schema, SchemaEnforcement(dto.SchemaEnforcement))
}

func encodeToolChoice(value ToolChoice) toolChoiceDTO {
	return toolChoiceDTO{Type: string(value.Kind()), Name: value.Name()}
}

func decodeToolChoice(dto toolChoiceDTO) (ToolChoice, error) {
	return NewToolChoice(ToolChoiceKind(dto.Type), dto.Name)
}

func encodeOutput(value OutputConstraints) outputDTO {
	dto := outputDTO{Format: string(value.Format()), SchemaEnforcement: string(value.Enforcement()), SchemaName: value.SchemaName(), MaxTokens: value.MaxTokens(), ParallelToolCalls: value.ParallelToolCalls()}
	if value.Schema().Kind() != canonicaljson.KindInvalid {
		dto.Schema = json.RawMessage(value.Schema().Bytes())
	}
	return dto
}

func decodeOutput(dto outputDTO) (OutputConstraints, error) {
	params := OutputParams{Format: OutputFormat(dto.Format), SchemaName: dto.SchemaName, MaxTokens: dto.MaxTokens, ParallelToolCalls: dto.ParallelToolCalls, Enforcement: SchemaEnforcement(dto.SchemaEnforcement)}
	if len(dto.Schema) != 0 {
		schema, err := canonicaljson.Parse(dto.Schema, canonicaljson.Limits{MaximumBytes: MaxToolSchemaBytes, MaximumDepth: MaxJSONDepth})
		if err != nil {
			return OutputConstraints{}, codecError(CodeInvalidJSON, "output.schema")
		}
		params.Schema = schema
	}
	return NewOutputConstraints(params)
}

func encodeSampling(value Sampling) samplingDTO {
	var dto samplingDTO
	if item, ok := value.Temperature(); ok {
		dto.Temperature = &item
	}
	if item, ok := value.TopP(); ok {
		dto.TopP = &item
	}
	if item, ok := value.Seed(); ok {
		dto.Seed = &item
	}
	return dto
}

func decodeSampling(dto samplingDTO) (Sampling, error) {
	return NewSampling(SamplingParams{Temperature: dto.Temperature, TopP: dto.TopP, Seed: dto.Seed})
}

func encodeContinuation(value *Continuation) *continuationDTO {
	if value == nil {
		return nil
	}
	return &continuationDTO{Provider: value.Provider(), Model: value.Model(), Data: value.Bytes()}
}

func decodeContinuation(dto *continuationDTO) (*Continuation, error) {
	if dto == nil {
		return nil, nil
	}
	value, err := NewContinuation(dto.Provider, dto.Model, dto.Data)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
