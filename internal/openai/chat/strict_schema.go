package chat

import (
	"encoding/json"
	"fmt"
	"sort"
)

// validateStrictSchema checks that a JSON Schema satisfies the Chat
// Completions strict-mode object rules before it is sent with strict: true:
// every object schema must set additionalProperties to false and list every
// declared property in required. Strict-mode endpoints reject other schemas
// upstream, so rejecting them here turns an opaque provider failure into a
// precise invalid-request error. Subschemas reachable through properties,
// items, prefixItems, anyOf, oneOf, allOf, $defs and definitions are checked.
func validateStrictSchema(path string, schema []byte) error {
	var root any
	if err := json.Unmarshal(schema, &root); err != nil {
		return invalid(path, "strict schema is not valid JSON")
	}
	return checkStrictNode(path, root, 0)
}

// maxStrictSchemaDepth bounds recursion; canonical schemas are already depth
// limited, so this only guards against pathological inputs.
const maxStrictSchemaDepth = 128

func checkStrictNode(path string, node any, depth int) error {
	if depth > maxStrictSchemaDepth {
		return invalid(path, "strict schema nesting is too deep")
	}
	schema, ok := node.(map[string]any)
	if !ok {
		// Boolean schemas and non-object values carry no object rules.
		return nil
	}
	properties, hasProperties := schema["properties"].(map[string]any)
	if isObjectSchema(schema) || hasProperties {
		if additional, ok := schema["additionalProperties"].(bool); !ok || additional {
			return invalid(path, "strict object schemas must set additionalProperties to false")
		}
		required := map[string]bool{}
		if list, ok := schema["required"].([]any); ok {
			for _, entry := range list {
				if name, ok := entry.(string); ok {
					required[name] = true
				}
			}
		}
		for _, name := range sortedKeys(properties) {
			if !required[name] {
				return invalid(path, fmt.Sprintf("strict object schemas must list property %q in required", name))
			}
		}
		for _, name := range sortedKeys(properties) {
			if err := checkStrictNode(path+".properties."+name, properties[name], depth+1); err != nil {
				return err
			}
		}
	}
	if err := checkStrictNode(path+".items", schema["items"], depth+1); err != nil {
		return err
	}
	for _, keyword := range []string{"prefixItems", "anyOf", "oneOf", "allOf"} {
		list, _ := schema[keyword].([]any)
		for index, entry := range list {
			if err := checkStrictNode(fmt.Sprintf("%s.%s[%d]", path, keyword, index), entry, depth+1); err != nil {
				return err
			}
		}
	}
	for _, keyword := range []string{"$defs", "definitions"} {
		definitions, _ := schema[keyword].(map[string]any)
		for _, name := range sortedKeys(definitions) {
			if err := checkStrictNode(path+"."+keyword+"."+name, definitions[name], depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// isObjectSchema reports whether the schema's type is, or includes, object.
func isObjectSchema(schema map[string]any) bool {
	switch kind := schema["type"].(type) {
	case string:
		return kind == "object"
	case []any:
		for _, entry := range kind {
			if entry == "object" {
				return true
			}
		}
	}
	return false
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
