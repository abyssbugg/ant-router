package anthropic

import (
	"fmt"

	"github.com/abyssbugg/ant-router/internal/canonical"
	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

func mapTools(tools []canonical.ToolDefinition) ([]sdk.ToolUnionParam, error) {
	result := make([]sdk.ToolUnionParam, 0, len(tools))
	for index, tool := range tools {
		schema, err := mapToolSchema(tool)
		if err != nil {
			return nil, requestMapError("tools[" + jsonIndex(index) + "].parameters")
		}
		mapped := sdk.ToolUnionParamOfTool(schema, tool.Name())
		if tool.Description() != "" {
			mapped.OfTool.Description = param.NewOpt(tool.Description())
		}
		mapped.OfTool.Strict = param.NewOpt(tool.Enforcement() == canonical.SchemaEnforcementStrict)
		result = append(result, mapped)
	}
	return result, nil
}

func mapToolSchema(tool canonical.ToolDefinition) (sdk.ToolInputSchemaParam, error) {
	value, err := canonicalAny(tool.InputSchema())
	if err != nil {
		return sdk.ToolInputSchemaParam{}, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return sdk.ToolInputSchemaParam{}, fmt.Errorf("tool schema is not an object")
	}
	schema := sdk.ToolInputSchemaParam{ExtraFields: make(map[string]any)}
	for key, item := range object {
		switch key {
		case "type":
			if item != "object" {
				return sdk.ToolInputSchemaParam{}, fmt.Errorf("tool schema type is not object")
			}
		case "properties":
			schema.Properties = item
		case "required":
			values, ok := item.([]any)
			if !ok {
				return sdk.ToolInputSchemaParam{}, fmt.Errorf("required is not an array")
			}
			for _, value := range values {
				text, ok := value.(string)
				if !ok {
					return sdk.ToolInputSchemaParam{}, fmt.Errorf("required item is not a string")
				}
				schema.Required = append(schema.Required, text)
			}
		default:
			schema.ExtraFields[key] = item
		}
	}
	return schema, nil
}

func mapToolChoice(choice canonical.ToolChoice, parallel bool) (sdk.ToolChoiceUnionParam, error) {
	disable := param.NewOpt(!parallel)
	switch choice.Kind() {
	case canonical.ToolChoiceKind("auto"):
		return sdk.ToolChoiceUnionParam{OfAuto: &sdk.ToolChoiceAutoParam{DisableParallelToolUse: disable}}, nil
	case canonical.ToolChoiceKind("none"):
		none := sdk.NewToolChoiceNoneParam()
		return sdk.ToolChoiceUnionParam{OfNone: &none}, nil
	case canonical.ToolChoiceKind("required"):
		return sdk.ToolChoiceUnionParam{OfAny: &sdk.ToolChoiceAnyParam{DisableParallelToolUse: disable}}, nil
	case canonical.ToolChoiceKind("specific"):
		return sdk.ToolChoiceUnionParam{OfTool: &sdk.ToolChoiceToolParam{Name: choice.Name(), DisableParallelToolUse: disable}}, nil
	default:
		return sdk.ToolChoiceUnionParam{}, requestMapError("tool_choice")
	}
}

func mapOutput(output canonical.OutputConstraints) (sdk.OutputConfigParam, error) {
	switch output.Format() {
	case canonical.OutputFormat("text"):
		return sdk.OutputConfigParam{}, nil
	case canonical.OutputFormat("json_object"):
		return sdk.OutputConfigParam{Format: sdk.JSONOutputFormatParam{Schema: map[string]any{"type": "object"}}}, nil
	case canonical.OutputFormat("json_schema"):
		if output.Enforcement() != canonical.SchemaEnforcementStrict {
			return sdk.OutputConfigParam{}, requestMapError("output.enforcement")
		}
		value, err := canonicalAny(output.Schema())
		if err != nil {
			return sdk.OutputConfigParam{}, requestMapError("output.schema")
		}
		schema, ok := value.(map[string]any)
		if !ok {
			return sdk.OutputConfigParam{}, requestMapError("output.schema")
		}
		return sdk.OutputConfigParam{Format: sdk.JSONOutputFormatParam{Schema: schema}}, nil
	default:
		return sdk.OutputConfigParam{}, requestMapError("output.format")
	}
}
