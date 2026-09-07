package control

import (
	"net/http"
	"time"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
)

type RouterOptions struct {
	Build        buildinfo.Info
	Status       StatusProvider
	Config       ConfigService
	CodexCatalog CodexCatalogProvider
	Now          func() time.Time
}

type Router struct {
	build        buildinfo.Info
	status       StatusProvider
	config       ConfigService
	codexCatalog CodexCatalogProvider
	now          func() time.Time
}

func NewRouter(options RouterOptions) (*Router, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Router{build: options.Build, status: options.Status, config: options.Config, codexCatalog: options.CodexCatalog, now: options.Now}, nil
}

func (r *Router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	var handler http.HandlerFunc
	allowedMethod := http.MethodGet
	switch request.URL.Path {
	case "/v1/health":
		handler = r.health
	case "/v1/version":
		handler = r.version
	case "/v1/status":
		handler = r.runtimeStatus
	case "/v1/codex/catalog":
		handler = r.codexCatalogCurrent
	case "/v1/config":
		handler = r.configCurrent
	case "/v1/config/validate":
		allowedMethod = http.MethodPost
		handler = r.configValidate
	case "/v1/config/apply":
		allowedMethod = http.MethodPost
		handler = r.configApply
	case "/v1/config/rollback":
		allowedMethod = http.MethodPost
		handler = r.configRollback
	default:
		_ = WriteError(writer, http.StatusNotFound, CodeNotFound, RequestIDFromContext(request.Context()))
		return
	}
	if request.Method != allowedMethod {
		writer.Header().Set("Allow", allowedMethod)
		_ = WriteError(writer, http.StatusMethodNotAllowed, CodeMethodNotAllowed, RequestIDFromContext(request.Context()))
		return
	}
	handler(writer, request)
}
