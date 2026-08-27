package canonical

import (
	"errors"
	"unicode/utf8"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

// ToolDefinition describes a portable callable tool.
type ToolDefinition struct {
	name        string
	description string
	inputSchema canonicaljson.Value
	enforcement SchemaEnforcement
}

// NewToolDefinition validates a tool name, description, and object schema using best-effort enforcement.
func NewToolDefinition(name, description string, schema canonicaljson.Value) (ToolDefinition, error) {
	return NewToolDefinitionWithEnforcement(name, description, schema, SchemaEnforcementBestEffort)
}

// NewToolDefinitionWithEnforcement validates a tool and its schema enforcement mode.
func NewToolDefinitionWithEnforcement(name, description string, schema canonicaljson.Value, enforcement SchemaEnforcement) (ToolDefinition, error) {
	if !validToolName(name) {
		code := CodeInvalidInvariant
		if len(name) > MaxToolNameBytes {
			code = CodeLimitExceeded
		}
		return ToolDefinition{}, itemError(code, "tool.name")
	}
	if !utf8.ValidString(description) {
		return ToolDefinition{}, itemError(CodeInvalidUTF8, "tool.description")
	}
	if len(description) > MaxToolDescriptionBytes {
		return ToolDefinition{}, itemError(CodeLimitExceeded, "tool.description")
	}
	if !schema.IsObject() || schema.Kind() == canonicaljson.KindInvalid {
		return ToolDefinition{}, itemError(CodeInvalidJSON, "tool.input_schema")
	}
	if len(schema.Bytes()) > MaxToolSchemaBytes {
		return ToolDefinition{}, itemError(CodeLimitExceeded, "tool.input_schema")
	}
	if enforcement == "" {
		enforcement = SchemaEnforcementBestEffort
	}
	if !enforcement.Valid() {
		return ToolDefinition{}, itemError(CodeInvalidInvariant, "tool.schema_enforcement")
	}
	return ToolDefinition{name: name, description: description, inputSchema: schema, enforcement: enforcement}, nil
}

// Name returns the portable tool name.
func (t ToolDefinition) Name() string { return t.name }

// Description returns the optional UTF-8 description.
func (t ToolDefinition) Description() string { return t.description }

// InputSchema returns the immutable canonical input schema.
func (t ToolDefinition) InputSchema() canonicaljson.Value { return t.inputSchema }

// Enforcement returns the tool schema enforcement mode.
func (t ToolDefinition) Enforcement() SchemaEnforcement { return t.enforcement }

// Valid reports whether the tool definition satisfies local invariants.
func (t ToolDefinition) Valid() bool {
	return validToolName(t.name) && utf8.ValidString(t.description) && len(t.description) <= MaxToolDescriptionBytes && t.inputSchema.IsObject() && len(t.inputSchema.Bytes()) <= MaxToolSchemaBytes && t.enforcement.Valid()
}

// ToolChoice controls canonical tool selection.
type ToolChoice struct {
	kind ToolChoiceKind
	name string
}

// NewToolChoice validates a tool-choice kind and optional specific name.
func NewToolChoice(kind ToolChoiceKind, name string) (ToolChoice, error) {
	if !kind.Valid() {
		return ToolChoice{}, itemError(CodeInvalidInvariant, "tool_choice.type")
	}
	if kind == ToolChoiceKind("specific") {
		if !validToolName(name) {
			return ToolChoice{}, itemError(CodeInvalidInvariant, "tool_choice.name")
		}
	} else if name != "" {
		return ToolChoice{}, itemError(CodeInvalidInvariant, "tool_choice.name")
	}
	return ToolChoice{kind: kind, name: name}, nil
}

// Kind returns the tool-choice discriminator.
func (c ToolChoice) Kind() ToolChoiceKind { return c.kind }

// Name returns the specific tool name when applicable.
func (c ToolChoice) Name() string { return c.name }

// Valid reports whether the tool choice satisfies local invariants.
func (c ToolChoice) Valid() bool {
	if !c.kind.Valid() {
		return false
	}
	if c.kind == ToolChoiceKind("specific") {
		return validToolName(c.name)
	}
	return c.name == ""
}

func validToolName(value string) bool {
	if len(value) < 1 || len(value) > MaxToolNameBytes {
		return false
	}
	first := value[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')) {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func itemError(code ErrorCode, path string) error {
	return &Error{Code: code, Path: path, Err: errors.New("item is invalid")}
}
