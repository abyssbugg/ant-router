package canonical

import (
	"errors"
	"unicode/utf8"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

// ToolDefinitionKind identifies a direct function or a namespace of functions.
type ToolDefinitionKind string

const (
	ToolDefinitionKindFunction  ToolDefinitionKind = "function"
	ToolDefinitionKindNamespace ToolDefinitionKind = "namespace"
)

// Valid reports whether the definition kind is supported.
func (k ToolDefinitionKind) Valid() bool {
	return k == ToolDefinitionKindFunction || k == ToolDefinitionKindNamespace
}

// ToolDefinition describes a portable direct function or function-only namespace.
type ToolDefinition struct {
	kind        ToolDefinitionKind
	name        string
	description string
	inputSchema canonicaljson.Value
	enforcement SchemaEnforcement
	tools       []ToolDefinition
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
	return ToolDefinition{kind: ToolDefinitionKindFunction, name: name, description: description, inputSchema: schema, enforcement: enforcement}, nil
}

// NewToolNamespace validates and defensively copies an ordered function-only namespace.
func NewToolNamespace(name, description string, tools []ToolDefinition) (ToolDefinition, error) {
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
	if len(tools) == 0 {
		return ToolDefinition{}, itemError(CodeInvalidInvariant, "tool.tools")
	}
	if len(tools) > MaxNamespaceTools {
		return ToolDefinition{}, itemError(CodeLimitExceeded, "tool.tools")
	}
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if !tool.Valid() || tool.Kind() != ToolDefinitionKindFunction {
			return ToolDefinition{}, itemError(CodeInvalidInvariant, "tool.tools")
		}
		if _, exists := seen[tool.Name()]; exists {
			return ToolDefinition{}, itemError(CodeInvalidInvariant, "tool.tools")
		}
		seen[tool.Name()] = struct{}{}
	}
	return ToolDefinition{kind: ToolDefinitionKindNamespace, name: name, description: description, tools: append([]ToolDefinition(nil), tools...)}, nil
}

// Kind returns whether this definition is a direct function or namespace.
func (t ToolDefinition) Kind() ToolDefinitionKind {
	if t.kind == "" && t.name != "" {
		return ToolDefinitionKindFunction
	}
	return t.kind
}

// Name returns the portable function or namespace name.
func (t ToolDefinition) Name() string { return t.name }

// Description returns the optional UTF-8 description.
func (t ToolDefinition) Description() string { return t.description }

// InputSchema returns the immutable canonical input schema.
func (t ToolDefinition) InputSchema() canonicaljson.Value { return t.inputSchema }

// Enforcement returns the function schema enforcement mode.
func (t ToolDefinition) Enforcement() SchemaEnforcement { return t.enforcement }

// Tools returns a defensive ordered copy of namespace children.
func (t ToolDefinition) Tools() []ToolDefinition { return append([]ToolDefinition(nil), t.tools...) }

// Valid reports whether the tool definition satisfies local invariants.
func (t ToolDefinition) Valid() bool {
	if !validToolName(t.name) || !utf8.ValidString(t.description) || len(t.description) > MaxToolDescriptionBytes {
		return false
	}
	switch t.Kind() {
	case ToolDefinitionKindFunction:
		return len(t.tools) == 0 && t.inputSchema.IsObject() && len(t.inputSchema.Bytes()) <= MaxToolSchemaBytes && t.enforcement.Valid()
	case ToolDefinitionKindNamespace:
		if len(t.tools) == 0 || len(t.tools) > MaxNamespaceTools || t.inputSchema.Kind() != canonicaljson.KindInvalid || t.enforcement != "" {
			return false
		}
		seen := make(map[string]struct{}, len(t.tools))
		for _, tool := range t.tools {
			if !tool.Valid() || tool.Kind() != ToolDefinitionKindFunction {
				return false
			}
			if _, ok := seen[tool.Name()]; ok {
				return false
			}
			seen[tool.Name()] = struct{}{}
		}
		return true
	default:
		return false
	}
}

// ToolChoice controls canonical tool selection.
type ToolChoice struct {
	kind      ToolChoiceKind
	namespace string
	name      string
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

// NewNamespacedToolChoice constructs a specific namespaced function choice.
func NewNamespacedToolChoice(namespace, name string) (ToolChoice, error) {
	if !validToolName(namespace) {
		return ToolChoice{}, itemError(CodeInvalidInvariant, "tool_choice.namespace")
	}
	if !validToolName(name) {
		return ToolChoice{}, itemError(CodeInvalidInvariant, "tool_choice.name")
	}
	return ToolChoice{kind: ToolChoiceKind("specific"), namespace: namespace, name: name}, nil
}

// Kind returns the tool-choice discriminator.
func (c ToolChoice) Kind() ToolChoiceKind { return c.kind }

// Namespace returns the namespace for a namespaced specific choice.
func (c ToolChoice) Namespace() string { return c.namespace }

// Name returns the specific child or direct tool name when applicable.
func (c ToolChoice) Name() string { return c.name }

// Valid reports whether the tool choice satisfies local invariants.
func (c ToolChoice) Valid() bool {
	if !c.kind.Valid() {
		return false
	}
	if c.kind == ToolChoiceKind("specific") {
		return validToolName(c.name) && (c.namespace == "" || validToolName(c.namespace))
	}
	return c.namespace == "" && c.name == ""
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
