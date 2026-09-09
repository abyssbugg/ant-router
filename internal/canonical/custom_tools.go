package canonical

import (
	"unicode"
	"unicode/utf8"
)

// CustomToolFormatKind identifies unconstrained text or grammar-constrained custom input.
type CustomToolFormatKind string

const (
	// CustomToolFormatKindText accepts unconstrained UTF-8 input.
	CustomToolFormatKindText CustomToolFormatKind = "text"
	// CustomToolFormatKindGrammar constrains input with a provider-portable grammar.
	CustomToolFormatKindGrammar CustomToolFormatKind = "grammar"
)

// Valid reports whether the custom input format kind is supported.
func (k CustomToolFormatKind) Valid() bool {
	return k == CustomToolFormatKindText || k == CustomToolFormatKindGrammar
}

// CustomGrammarSyntax identifies a supported custom-tool grammar language.
type CustomGrammarSyntax string

const (
	// CustomGrammarSyntaxLark identifies a Lark grammar.
	CustomGrammarSyntaxLark CustomGrammarSyntax = "lark"
	// CustomGrammarSyntaxRegex identifies a regular-expression grammar.
	CustomGrammarSyntaxRegex CustomGrammarSyntax = "regex"
)

// Valid reports whether the grammar syntax is supported.
func (s CustomGrammarSyntax) Valid() bool {
	return s == CustomGrammarSyntaxLark || s == CustomGrammarSyntaxRegex
}

// CustomToolFormat is an immutable custom-tool input format.
type CustomToolFormat struct {
	kind       CustomToolFormatKind
	syntax     CustomGrammarSyntax
	definition string
}

// NewCustomTextFormat constructs the unconstrained text format.
func NewCustomTextFormat() CustomToolFormat {
	return CustomToolFormat{kind: CustomToolFormatKindText}
}

// NewCustomGrammarFormat validates and constructs a grammar-constrained format.
func NewCustomGrammarFormat(syntax CustomGrammarSyntax, definition string) (CustomToolFormat, error) {
	if !syntax.Valid() {
		return CustomToolFormat{}, itemError(CodeInvalidInvariant, "custom_tool.format.syntax")
	}
	if !utf8.ValidString(definition) {
		return CustomToolFormat{}, itemError(CodeInvalidUTF8, "custom_tool.format.definition")
	}
	if len(definition) == 0 {
		return CustomToolFormat{}, itemError(CodeInvalidInvariant, "custom_tool.format.definition")
	}
	if len(definition) > MaxCustomGrammarBytes {
		return CustomToolFormat{}, itemError(CodeLimitExceeded, "custom_tool.format.definition")
	}
	if !validCustomGrammarCharacters(definition) {
		return CustomToolFormat{}, itemError(CodeInvalidInvariant, "custom_tool.format.definition")
	}
	return CustomToolFormat{kind: CustomToolFormatKindGrammar, syntax: syntax, definition: definition}, nil
}

// Kind returns the custom input format discriminator.
func (f CustomToolFormat) Kind() CustomToolFormatKind { return f.kind }

// Syntax returns the grammar syntax, or empty for unconstrained text.
func (f CustomToolFormat) Syntax() CustomGrammarSyntax { return f.syntax }

// Definition returns the grammar definition, or empty for unconstrained text.
func (f CustomToolFormat) Definition() string { return f.definition }

// Valid reports whether the custom input format satisfies local invariants.
func (f CustomToolFormat) Valid() bool {
	switch f.kind {
	case CustomToolFormatKindText:
		return f.syntax == "" && f.definition == ""
	case CustomToolFormatKindGrammar:
		return f.syntax.Valid() && utf8.ValidString(f.definition) && len(f.definition) > 0 && len(f.definition) <= MaxCustomGrammarBytes && validCustomGrammarCharacters(f.definition)
	default:
		return false
	}
}

// NewCustomTool validates a custom tool using unconstrained text input.
func NewCustomTool(name, description string) (ToolDefinition, error) {
	return NewCustomToolWithFormat(name, description, NewCustomTextFormat())
}

// NewCustomToolWithFormat validates a custom tool and input format.
func NewCustomToolWithFormat(name, description string, format CustomToolFormat) (ToolDefinition, error) {
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
	if !format.Valid() {
		return ToolDefinition{}, itemError(CodeInvalidInvariant, "tool.custom_format")
	}
	return ToolDefinition{kind: ToolDefinitionKindCustom, name: name, description: description, customFormat: format}, nil
}

func validCustomGrammarCharacters(definition string) bool {
	for _, character := range definition {
		if unicode.IsControl(character) && character != '\t' && character != '\n' && character != '\r' {
			return false
		}
	}
	return true
}
