package responses

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func decodeTools(raw json.RawMessage) ([]canonical.ToolDefinition, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || len(values) > canonical.MaxTools {
		return nil, newError(CodeInvalidField, "tools", errors.New("tools are invalid"))
	}
	tools := make([]canonical.ToolDefinition, 0, len(values))
	for index, value := range values {
		path := fmt.Sprintf("tools[%d]", index)
		fields, err := strictObject(value, path, "type", "name", "description", "parameters", "strict")
		if err != nil {
			return nil, err
		}
		kind, err := requiredString(fields, "type", path+".type")
		if err != nil {
			return nil, err
		}
		if kind != "function" {
			return nil, newError(CodeUnsupportedField, path+".type", errors.New("tool type is unsupported"))
		}
		name, err := requiredString(fields, "name", path+".name")
		if err != nil {
			return nil, err
		}
		description := ""
		if rawDescription, ok := fields["description"]; ok {
			if json.Unmarshal(rawDescription, &description) != nil {
				return nil, newError(CodeInvalidField, path+".description", errors.New("description is invalid"))
			}
		}
		rawSchema, ok := fields["parameters"]
		if !ok {
			return nil, newError(CodeInvalidField, path+".parameters", errors.New("parameters are required"))
		}
		schema, err := canonicaljson.Parse(rawSchema, canonicaljson.DefaultLimits())
		if err != nil || !schema.IsObject() {
			return nil, newError(CodeInvalidField, path+".parameters", errors.New("parameters are invalid"))
		}
		enforcement := canonical.SchemaEnforcementBestEffort
		if rawStrict, ok := fields["strict"]; ok {
			var strict bool
			if json.Unmarshal(rawStrict, &strict) != nil {
				return nil, newError(CodeInvalidField, path+".strict", errors.New("strict is invalid"))
			}
			if strict {
				enforcement = canonical.SchemaEnforcementStrict
			}
		}
		tool, err := canonical.NewToolDefinitionWithEnforcement(name, description, schema, enforcement)
		if err != nil {
			return nil, newError(CodeInvalidField, path, errors.New("tool is invalid"))
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func decodeToolChoice(raw json.RawMessage, tools []canonical.ToolDefinition) (canonical.ToolChoice, error) {
	kind := canonical.ToolChoiceKind("auto")
	name := ""
	if len(raw) != 0 {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && trimmed[0] == '"' {
			var value string
			if json.Unmarshal(raw, &value) != nil || (value != "auto" && value != "none" && value != "required") {
				return canonical.ToolChoice{}, newError(CodeInvalidField, "tool_choice", errors.New("tool choice is invalid"))
			}
			kind = canonical.ToolChoiceKind(value)
		} else {
			fields, err := strictObject(raw, "tool_choice", "type", "name")
			if err != nil {
				return canonical.ToolChoice{}, err
			}
			value, err := requiredString(fields, "type", "tool_choice.type")
			if err != nil {
				return canonical.ToolChoice{}, err
			}
			if value != "function" {
				return canonical.ToolChoice{}, newError(CodeUnsupportedField, "tool_choice.type", errors.New("tool choice type is unsupported"))
			}
			name, err = requiredString(fields, "name", "tool_choice.name")
			if err != nil {
				return canonical.ToolChoice{}, err
			}
			kind = canonical.ToolChoiceKind("specific")
		}
	}
	choice, err := canonical.NewToolChoice(kind, name)
	if err != nil {
		return canonical.ToolChoice{}, newError(CodeInvalidField, "tool_choice", errors.New("tool choice is invalid"))
	}
	if kind == canonical.ToolChoiceKind("required") && len(tools) == 0 {
		return canonical.ToolChoice{}, newError(CodeInvalidField, "tool_choice", errors.New("required tool choice needs tools"))
	}
	if kind == canonical.ToolChoiceKind("specific") {
		declared := false
		for _, tool := range tools {
			if tool.Name() == name {
				declared = true
				break
			}
		}
		if !declared {
			return canonical.ToolChoice{}, newError(CodeInvalidField, "tool_choice.name", errors.New("tool is not declared"))
		}
	}
	return choice, nil
}

func decodeOutput(raw json.RawMessage, maxTokens int, parallel bool) (canonical.OutputConstraints, error) {
	params := canonical.OutputParams{Format: canonical.OutputFormat("text"), MaxTokens: maxTokens, ParallelToolCalls: parallel}
	if len(raw) != 0 {
		textFields, err := strictObject(raw, "text", "format")
		if err != nil {
			return canonical.OutputConstraints{}, err
		}
		rawFormat, ok := textFields["format"]
		if !ok {
			return canonical.OutputConstraints{}, newError(CodeInvalidField, "text.format", errors.New("format is required"))
		}
		formatFields, err := strictObject(rawFormat, "text.format", "type", "name", "schema", "strict")
		if err != nil {
			return canonical.OutputConstraints{}, err
		}
		kind, err := requiredString(formatFields, "type", "text.format.type")
		if err != nil {
			return canonical.OutputConstraints{}, err
		}
		switch kind {
		case "text":
			if err := rejectFields(formatFields, "text.format", "type"); err != nil {
				return canonical.OutputConstraints{}, err
			}
		case "json_object":
			if err := rejectFields(formatFields, "text.format", "type"); err != nil {
				return canonical.OutputConstraints{}, err
			}
			params.Format = canonical.OutputFormat("json_object")
		case "json_schema":
			name, err := requiredString(formatFields, "name", "text.format.name")
			if err != nil {
				return canonical.OutputConstraints{}, err
			}
			rawSchema, ok := formatFields["schema"]
			if !ok {
				return canonical.OutputConstraints{}, newError(CodeInvalidField, "text.format.schema", errors.New("schema is required"))
			}
			schema, err := canonicaljson.Parse(rawSchema, canonicaljson.DefaultLimits())
			if err != nil || !schema.IsObject() {
				return canonical.OutputConstraints{}, newError(CodeInvalidField, "text.format.schema", errors.New("schema is invalid"))
			}
			enforcement := canonical.SchemaEnforcementBestEffort
			if rawStrict, ok := formatFields["strict"]; ok {
				var strict bool
				if json.Unmarshal(rawStrict, &strict) != nil {
					return canonical.OutputConstraints{}, newError(CodeInvalidField, "text.format.strict", errors.New("strict is invalid"))
				}
				if strict {
					enforcement = canonical.SchemaEnforcementStrict
				}
			}
			params.Format, params.SchemaName, params.Schema, params.Enforcement = canonical.OutputFormat("json_schema"), name, schema, enforcement
		default:
			return canonical.OutputConstraints{}, newError(CodeUnsupportedField, "text.format.type", errors.New("format type is unsupported"))
		}
	}
	output, err := canonical.NewOutputConstraints(params)
	if err != nil {
		return canonical.OutputConstraints{}, newError(CodeInvalidField, "text", errors.New("output controls are invalid"))
	}
	return output, nil
}

func decodeReasoning(raw json.RawMessage) (canonical.ReasoningSummaryMode, error) {
	if len(raw) == 0 {
		return canonical.ReasoningSummaryNone, nil
	}
	fields, err := strictObject(raw, "reasoning", "summary")
	if err != nil {
		return "", err
	}
	summary, err := requiredString(fields, "summary", "reasoning.summary")
	if err != nil {
		return "", err
	}
	mode := canonical.ReasoningSummaryMode(summary)
	if mode != canonical.ReasoningSummaryAuto && mode != canonical.ReasoningSummaryConcise && mode != canonical.ReasoningSummaryDetailed {
		return "", newError(CodeInvalidField, "reasoning.summary", errors.New("reasoning summary is invalid"))
	}
	return mode, nil
}
