package daemon

import (
	"context"

	"github.com/abyssbugg/ant-router/internal/codexcatalog"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/execution"
)

func (r *providerRuntime) codexCatalog(ctx context.Context) (control.CodexCatalog, error) {
	if r == nil || r.registry == nil || ctx == nil || r.configGeneration == 0 {
		return control.CodexCatalog{}, execution.NewError(execution.CodeInternal, "providers.codex_catalog", 0, nil)
	}
	models, err := r.registry.Models(ctx)
	if err != nil {
		return control.CodexCatalog{}, err
	}
	projections := make([]codexcatalog.Projection, 0, len(models))
	for _, model := range models {
		metadata, present := r.codexMetadata[model.ID()]
		if !present || !metadata.Enabled {
			continue
		}
		values := model.Capabilities().Values()
		capabilities := make([]string, len(values))
		for index, value := range values {
			capabilities[index] = string(value)
		}
		projections = append(projections, codexcatalog.Projection{Alias: model.ID(), Capabilities: capabilities, Metadata: metadata})
	}
	encoded, err := codexcatalog.Generate(projections)
	if err != nil {
		return control.CodexCatalog{}, execution.NewError(execution.CodeUnavailable, "providers.codex_catalog", 0, err)
	}
	return control.NewCodexCatalog(r.configGeneration, encoded)
}
