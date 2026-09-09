package canonical

import (
	"fmt"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func testCustomTool(t *testing.T, name string) ToolDefinition {
	t.Helper()
	tool, err := NewCustomTool(name, "freeform tool")
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestCustomToolDefaultsToTextFormat(t *testing.T) {
	tool := testCustomTool(t, "apply_patch")
	format := tool.CustomFormat()
	if !tool.Valid() || tool.Kind() != ToolDefinitionKindCustom || tool.Name() != "apply_patch" || format.Kind() != CustomToolFormatKindText || !format.Valid() {
		t.Fatalf("custom tool = %#v, format = %#v", tool, format)
	}
	if tool.InputSchema().Kind() != canonicaljson.KindInvalid || tool.Enforcement() != "" || len(tool.Tools()) != 0 {
		t.Fatal("custom tool retained another tool variant")
	}
}

func TestCustomToolSupportsLarkAndRegexGrammar(t *testing.T) {
	for _, syntax := range []CustomGrammarSyntax{CustomGrammarSyntaxLark, CustomGrammarSyntaxRegex} {
		t.Run(string(syntax), func(t *testing.T) {
			definition := "start: WORD\n%import common.WORD"
			format, err := NewCustomGrammarFormat(syntax, definition)
			if err != nil || !format.Valid() || format.Kind() != CustomToolFormatKindGrammar || format.Syntax() != syntax || format.Definition() != definition {
				t.Fatalf("format = %#v, err = %v", format, err)
			}
			tool, err := NewCustomToolWithFormat("edit", "edit a file", format)
			if err != nil || !tool.Valid() || tool.CustomFormat().Definition() != definition {
				t.Fatalf("tool = %#v, err = %v", tool, err)
			}
		})
	}
}

func TestCustomToolRejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name string
		make func() error
		code ErrorCode
	}{
		{"name", func() error { _, err := NewCustomTool("bad name", ""); return err }, CodeInvalidInvariant},
		{"description utf8", func() error { _, err := NewCustomTool("edit", string([]byte{0xff})); return err }, CodeInvalidUTF8},
		{"description limit", func() error {
			_, err := NewCustomTool("edit", strings.Repeat("d", MaxToolDescriptionBytes+1))
			return err
		}, CodeLimitExceeded},
		{"syntax", func() error {
			_, err := NewCustomGrammarFormat(CustomGrammarSyntax("other"), "start: WORD")
			return err
		}, CodeInvalidInvariant},
		{"empty grammar", func() error { _, err := NewCustomGrammarFormat(CustomGrammarSyntaxLark, ""); return err }, CodeInvalidInvariant},
		{"grammar utf8", func() error {
			_, err := NewCustomGrammarFormat(CustomGrammarSyntaxLark, string([]byte{0xff}))
			return err
		}, CodeInvalidUTF8},
		{"grammar control", func() error { _, err := NewCustomGrammarFormat(CustomGrammarSyntaxLark, "start:\x00WORD"); return err }, CodeInvalidInvariant},
		{"grammar del control", func() error { _, err := NewCustomGrammarFormat(CustomGrammarSyntaxLark, "start:\x7fWORD"); return err }, CodeInvalidInvariant},
		{"grammar unicode control", func() error {
			_, err := NewCustomGrammarFormat(CustomGrammarSyntaxLark, "start:\u0085WORD")
			return err
		}, CodeInvalidInvariant},
		{"grammar limit", func() error {
			_, err := NewCustomGrammarFormat(CustomGrammarSyntaxLark, strings.Repeat("x", MaxCustomGrammarBytes+1))
			return err
		}, CodeLimitExceeded},
		{"invalid format", func() error { _, err := NewCustomToolWithFormat("edit", "", CustomToolFormat{}); return err }, CodeInvalidInvariant},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.make(); codeOfCanonical(err) != test.code {
				t.Fatalf("code = %q, want %q; error = %v", codeOfCanonical(err), test.code, err)
			}
		})
	}
}

func TestNamespaceAcceptsFunctionAndCustomChildren(t *testing.T) {
	function := testNamespaceFunction(t, "read_file")
	custom := testCustomTool(t, "apply_patch")
	namespace, err := NewToolNamespace("workspace", "workspace tools", []ToolDefinition{function, custom})
	if err != nil || !namespace.Valid() {
		t.Fatalf("namespace = %#v, err = %v", namespace, err)
	}
	children := namespace.Tools()
	if len(children) != 2 || children[0].Kind() != ToolDefinitionKindFunction || children[1].Kind() != ToolDefinitionKindCustom {
		t.Fatalf("children = %#v", children)
	}
	children[1] = ToolDefinition{}
	if !namespace.Valid() || namespace.Tools()[1].Kind() != ToolDefinitionKindCustom {
		t.Fatal("namespace children were not immutable")
	}
	if _, err := NewToolNamespace("workspace", "", []ToolDefinition{function, testCustomTool(t, "read_file")}); err == nil {
		t.Fatal("cross-kind duplicate child name accepted")
	}
	if _, err := NewToolNamespace("outer", "", []ToolDefinition{NewHostedWebSearchTool()}); err == nil {
		t.Fatal("hosted search accepted inside namespace")
	}
}

func TestRequestDerivesCustomAndNamespaceCapabilities(t *testing.T) {
	custom := testCustomTool(t, "apply_patch")
	namespace, err := NewToolNamespace("workspace", "", []ToolDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	message, _ := NewMessage(testItemID(t, 80), Role("user"), []ContentBlock{testTextBlock(t, "hello")})
	item, _ := NewMessageItem(message)
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 80), Model: "model", Input: []Item{item}, Tools: []ToolDefinition{namespace}})
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []Capability{CapabilityText, CapabilityTools, CapabilityNamespaceTools, CapabilityCustomTools} {
		if !request.Capabilities().Has(capability) {
			t.Fatalf("capability %q missing", capability)
		}
	}
}

func TestRequestCountsCustomToolsTowardAggregateLimit(t *testing.T) {
	tools := make([]ToolDefinition, 0, 3)
	for namespaceIndex := 0; namespaceIndex < 3; namespaceIndex++ {
		children := make([]ToolDefinition, 0, 86)
		for childIndex := 0; childIndex < 86; childIndex++ {
			children = append(children, testCustomTool(t, fmt.Sprintf("tool%d_%d", namespaceIndex, childIndex)))
		}
		namespace, err := NewToolNamespace(fmt.Sprintf("ns%d", namespaceIndex), "", children)
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, namespace)
	}
	message, _ := NewMessage(testItemID(t, 81), Role("user"), []ContentBlock{testTextBlock(t, "hello")})
	item, _ := NewMessageItem(message)
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 81), Model: "model", Input: []Item{item}, Tools: tools}); err == nil {
		t.Fatal("aggregate custom tool limit accepted")
	}
}

func TestCustomGrammarErrorsRedactDefinition(t *testing.T) {
	const secret = "grammar-secret-7c54d37efaf7488b"
	_, err := NewCustomGrammarFormat(CustomGrammarSyntax("unsupported"), secret)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked grammar definition: %v", err)
	}
}

func FuzzCustomGrammarFormat(f *testing.F) {
	f.Add("lark", "start: WORD\n%import common.WORD")
	f.Add("regex", `[a-z]+`)
	f.Add("lark", "start:\x00WORD")
	f.Fuzz(func(t *testing.T, syntaxValue, definition string) {
		if len(syntaxValue) > 32 || len(definition) > MaxCustomGrammarBytes+1 {
			return
		}
		format, err := NewCustomGrammarFormat(CustomGrammarSyntax(syntaxValue), definition)
		if err != nil {
			return
		}
		if !format.Valid() || format.Syntax() != CustomGrammarSyntax(syntaxValue) || format.Definition() != definition {
			t.Fatalf("valid grammar did not round trip")
		}
	})
}
