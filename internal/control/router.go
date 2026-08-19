package control

import (
	"net/http"
	"time"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
)

type RouterOptions struct {
	Build  buildinfo.Info
	Status StatusProvider
	Now    func() time.Time
}

type Router struct {
	build  buildinfo.Info
	status StatusProvider
	now    func() time.Time
}

func NewRouter(options RouterOptions) (*Router, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Router{build: options.Build, status: options.Status, now: options.Now}, nil
}

func (r *Router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	var handler http.HandlerFunc
	switch request.URL.Path {
	case "/v1/health":
		handler = r.health
	case "/v1/version":
		handler = r.version
	case "/v1/status":
		handler = r.runtimeStatus
	default:
		_ = WriteError(writer, http.StatusNotFound, CodeNotFound, RequestIDFromContext(request.Context()))
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		_ = WriteError(writer, http.StatusMethodNotAllowed, CodeMethodNotAllowed, RequestIDFromContext(request.Context()))
		return
	}
	handler(writer, request)
}
