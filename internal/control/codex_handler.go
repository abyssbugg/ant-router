package control

import (
	"context"
	"errors"
	"net/http"
)

func (r *Router) codexCatalogCurrent(writer http.ResponseWriter, request *http.Request) {
	requestID := RequestIDFromContext(request.Context())
	if r.codexCatalog == nil {
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeServiceUnavailable, requestID)
		return
	}
	catalog, err := callCodexCatalogProvider(request.Context(), r.codexCatalog)
	if err != nil {
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeServiceUnavailable, requestID)
		return
	}
	validated, err := NewCodexCatalog(catalog.Generation, catalog.JSON)
	if err != nil {
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeServiceUnavailable, requestID)
		return
	}
	_ = WriteJSON(writer, http.StatusOK, validated.response())
}

func callCodexCatalogProvider(ctx context.Context, provider CodexCatalogProvider) (catalog CodexCatalog, err error) {
	defer func() {
		if recover() != nil {
			catalog = CodexCatalog{}
			err = errors.New("Codex catalog provider failed")
		}
	}()
	return provider.CodexCatalog(ctx)
}
