package canonical

import "github.com/abyssbugg/ant-router/internal/canonicaljson"

type OutputParams struct {
	Format            OutputFormat
	SchemaName        string
	Schema            canonicaljson.Value
	MaxTokens         int
	ParallelToolCalls bool
}

// OutputConstraints contains provider-neutral output requirements.
type OutputConstraints struct {
	format            OutputFormat
	schemaName        string
	schema            canonicaljson.Value
	maxTokens         int
	parallelToolCalls bool
}

// NewOutputConstraints validates output format, schema, and limits.
func NewOutputConstraints(params OutputParams) (OutputConstraints, error) {
	if !params.Format.Valid() {
		return OutputConstraints{}, requestError(CodeInvalidInvariant, "output.format")
	}
	if params.MaxTokens < 0 || params.MaxTokens > MaxOutputTokens {
		return OutputConstraints{}, requestError(CodeLimitExceeded, "output.max_tokens")
	}
	if params.Format == OutputFormat("json_schema") {
		if !validToolName(params.SchemaName) || len(params.SchemaName) > MaxSchemaNameBytes || !params.Schema.IsObject() || len(params.Schema.Bytes()) > MaxToolSchemaBytes {
			return OutputConstraints{}, requestError(CodeInvalidInvariant, "output.schema")
		}
	} else if params.SchemaName != "" || params.Schema.Kind() != canonicaljson.KindInvalid {
		return OutputConstraints{}, requestError(CodeInvalidInvariant, "output.schema")
	}
	return OutputConstraints{format: params.Format, schemaName: params.SchemaName, schema: params.Schema, maxTokens: params.MaxTokens, parallelToolCalls: params.ParallelToolCalls}, nil
}

func (o OutputConstraints) Format() OutputFormat        { return o.format }
func (o OutputConstraints) SchemaName() string          { return o.schemaName }
func (o OutputConstraints) Schema() canonicaljson.Value { return o.schema }
func (o OutputConstraints) MaxTokens() int              { return o.maxTokens }
func (o OutputConstraints) ParallelToolCalls() bool     { return o.parallelToolCalls }
func (o OutputConstraints) Valid() bool {
	if !o.format.Valid() || o.maxTokens < 0 || o.maxTokens > MaxOutputTokens {
		return false
	}
	if o.format == OutputFormat("json_schema") {
		return validToolName(o.schemaName) && len(o.schemaName) <= MaxSchemaNameBytes && o.schema.IsObject() && len(o.schema.Bytes()) <= MaxToolSchemaBytes
	}
	return o.schemaName == "" && o.schema.Kind() == canonicaljson.KindInvalid
}
