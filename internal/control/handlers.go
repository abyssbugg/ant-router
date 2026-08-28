package control

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func (r *Router) health(writer http.ResponseWriter, request *http.Request) {
	_ = WriteJSON(writer, http.StatusOK, HealthResponse{Status: "ok", APIVersion: APIVersion})
}

func (r *Router) version(writer http.ResponseWriter, request *http.Request) {
	response := VersionResponse{
		Name:       "ant-router",
		Version:    r.build.Version,
		Commit:     r.build.Commit,
		BuiltAt:    r.build.Date,
		APIVersion: APIVersion,
	}
	_ = WriteJSON(writer, http.StatusOK, response)
}

func (r *Router) runtimeStatus(writer http.ResponseWriter, request *http.Request) {
	requestID := RequestIDFromContext(request.Context())
	if r.status == nil {
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeServiceUnavailable, requestID)
		return
	}
	status, err := callStatusProvider(request.Context(), r.status)
	if err != nil || status.Validate() != nil {
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeServiceUnavailable, requestID)
		return
	}
	_ = WriteJSON(writer, http.StatusOK, buildStatusResponse(status, r.now()))
}

func callStatusProvider(ctx context.Context, provider StatusProvider) (status RuntimeStatus, err error) {
	defer func() {
		if recover() != nil {
			status = RuntimeStatus{}
			err = errors.New("status provider failed")
		}
	}()
	return provider.Status(ctx)
}

func buildStatusResponse(status RuntimeStatus, now time.Time) StatusResponse {
	uptime := now.Sub(status.StartedAt)
	if uptime < 0 {
		uptime = 0
	}
	var configCopy *ConfigSummary
	if status.Config != nil {
		copyValue := *status.Config
		configCopy = &copyValue
	}
	var dataPlaneCopy *DataPlaneSummary
	if status.DataPlane != nil {
		copyValue := *status.DataPlane
		dataPlaneCopy = &copyValue
	}
	return StatusResponse{
		Instance:      status.Instance,
		DaemonState:   status.DaemonState,
		StartedAt:     status.StartedAt,
		UptimeSeconds: int64(uptime.Seconds()),
		Config:        configCopy,
		DataPlane:     dataPlaneCopy,
	}
}
