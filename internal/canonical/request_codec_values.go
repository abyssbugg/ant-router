package canonical

import (
	"encoding/json"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func encodeTool(value ToolDefinition) toolDTO {
	if value.Kind() == ToolDefinitionKindWebSearch {
		options := value.WebSearchOptions()
		dto := toolDTO{Type: string(ToolDefinitionKindWebSearch), Name: value.Name(), SearchContextSize: string(options.SearchContextSize()), SearchContentTypes: options.SearchContentTypes()}
		if v, ok := options.ExternalWebAccess(); ok {
			dto.ExternalWebAccess = &v
		}
		if v, ok := options.IndexedWebAccess(); ok {
			dto.IndexedWebAccess = &v
		}
		if value := options.Filters(); value.Kind() != canonicaljson.KindInvalid {
			dto.Filters = json.RawMessage(value.Bytes())
		}
		if value := options.UserLocation(); value.Kind() != canonicaljson.KindInvalid {
			dto.UserLocation = json.RawMessage(value.Bytes())
		}
		return dto
	}
	if value.Kind() == ToolDefinitionKindNamespace {
		children := value.Tools()
		encoded := make([]toolDTO, 0, len(children))
		for _, child := range children {
			encoded = append(encoded, encodeTool(child))
		}
		return toolDTO{Type: string(ToolDefinitionKindNamespace), Name: value.Name(), Description: value.Description(), Tools: encoded}
	}
	return toolDTO{Name: value.Name(), Description: value.Description(), InputSchema: json.RawMessage(value.InputSchema().Bytes()), SchemaEnforcement: string(value.Enforcement())}
}

func decodeTool(dto toolDTO) (ToolDefinition, error) {
	if dto.Type == string(ToolDefinitionKindWebSearch) {
		if dto.Name != "web_search" || dto.Description != "" || len(dto.InputSchema) != 0 || dto.SchemaEnforcement != "" || len(dto.Tools) != 0 {
			return ToolDefinition{}, codecError(CodeInvalidInvariant, "tool")
		}
		filters, err := decodeOptionalHostedObject(dto.Filters)
		if err != nil {
			return ToolDefinition{}, err
		}
		location, err := decodeOptionalHostedObject(dto.UserLocation)
		if err != nil {
			return ToolDefinition{}, err
		}
		options, err := NewHostedWebSearchOptions(HostedWebSearchOptionsParams{ExternalWebAccess: dto.ExternalWebAccess, IndexedWebAccess: dto.IndexedWebAccess, Filters: filters, UserLocation: location, SearchContextSize: HostedSearchContextSize(dto.SearchContextSize), SearchContentTypes: dto.SearchContentTypes})
		if err != nil {
			return ToolDefinition{}, err
		}
		return NewHostedWebSearchToolWithOptions(options)
	}
	if dto.Type == string(ToolDefinitionKindNamespace) {
		if len(dto.InputSchema) != 0 || dto.SchemaEnforcement != "" {
			return ToolDefinition{}, codecError(CodeInvalidInvariant, "tool")
		}
		children := make([]ToolDefinition, 0, len(dto.Tools))
		for _, childDTO := range dto.Tools {
			if childDTO.Type == string(ToolDefinitionKindNamespace) || len(childDTO.Tools) != 0 {
				return ToolDefinition{}, codecError(CodeInvalidInvariant, "tool.tools")
			}
			child, err := decodeTool(childDTO)
			if err != nil {
				return ToolDefinition{}, err
			}
			children = append(children, child)
		}
		return NewToolNamespace(dto.Name, dto.Description, children)
	}
	if dto.Type != "" && dto.Type != string(ToolDefinitionKindFunction) || len(dto.Tools) != 0 {
		return ToolDefinition{}, codecError(CodeInvalidInvariant, "tool.type")
	}
	schema, err := canonicaljson.Parse(dto.InputSchema, canonicaljson.Limits{MaximumBytes: MaxToolSchemaBytes, MaximumDepth: MaxJSONDepth})
	if err != nil {
		return ToolDefinition{}, codecError(CodeInvalidJSON, "tool.input_schema")
	}
	return NewToolDefinitionWithEnforcement(dto.Name, dto.Description, schema, SchemaEnforcement(dto.SchemaEnforcement))
}

func decodeOptionalHostedObject(raw json.RawMessage) (canonicaljson.Value, error) {
	if len(raw) == 0 {
		return canonicaljson.Value{}, nil
	}
	value, err := canonicaljson.Parse(raw, canonicaljson.Limits{MaximumBytes: MaxHostedSearchOptionsBytes, MaximumDepth: MaxJSONDepth})
	if err != nil || !value.IsObject() {
		return canonicaljson.Value{}, codecError(CodeInvalidJSON, "tool.web_search")
	}
	return value, nil
}

func encodeToolChoice(value ToolChoice) toolChoiceDTO {
	return toolChoiceDTO{Type: string(value.Kind()), Namespace: value.Namespace(), Name: value.Name()}
}

func decodeToolChoice(dto toolChoiceDTO) (ToolChoice, error) {
	if dto.Namespace != "" {
		if ToolChoiceKind(dto.Type) != ToolChoiceKind("specific") {
			return ToolChoice{}, codecError(CodeInvalidInvariant, "tool_choice.namespace")
		}
		return NewNamespacedToolChoice(dto.Namespace, dto.Name)
	}
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
