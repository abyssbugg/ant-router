package control

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/configservice"
)

func (r *Router) configCurrent(writer http.ResponseWriter, request *http.Request) {
	if r.config == nil {
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeConfigurationUnavailable, RequestIDFromContext(request.Context()))
		return
	}
	_ = WriteJSON(writer, http.StatusOK, currentResponse(r.config.Current()))
}

func (r *Router) configValidate(writer http.ResponseWriter, request *http.Request) {
	if r.config == nil {
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeConfigurationUnavailable, RequestIDFromContext(request.Context()))
		return
	}
	var body ConfigValidateRequest
	if err := decodeRequestJSON(request.Body, &body); err != nil {
		_ = WriteError(writer, http.StatusBadRequest, CodeConfigurationInvalid, RequestIDFromContext(request.Context()))
		return
	}
	result, err := r.config.Validate(request.Context(), []byte(body.TOML))
	if err != nil {
		writeConfigServiceError(writer, request, err)
		return
	}
	_ = WriteJSON(writer, http.StatusOK, ConfigValidateResponse{Revision: result.Revision.String(), BaseGeneration: result.BaseGeneration, CanonicalSize: result.CanonicalSize})
}

func (r *Router) configApply(writer http.ResponseWriter, request *http.Request) {
	if r.config == nil {
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeConfigurationUnavailable, RequestIDFromContext(request.Context()))
		return
	}
	var body ConfigApplyRequest
	if err := decodeRequestJSON(request.Body, &body); err != nil {
		_ = WriteError(writer, http.StatusBadRequest, CodeConfigurationInvalid, RequestIDFromContext(request.Context()))
		return
	}
	result, err := r.config.Apply(request.Context(), configservice.ApplyRequest{TOML: []byte(body.TOML), ExpectedRevision: config.RevisionID(body.ExpectedRevision), ExpectedGeneration: body.ExpectedGeneration})
	if err != nil {
		writeConfigServiceError(writer, request, err)
		return
	}
	_ = WriteJSON(writer, http.StatusOK, mutationResponse(result))
}

func (r *Router) configRollback(writer http.ResponseWriter, request *http.Request) {
	if r.config == nil {
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeConfigurationUnavailable, RequestIDFromContext(request.Context()))
		return
	}
	if request.ContentLength != 0 || len(request.TransferEncoding) != 0 {
		var empty struct{}
		if err := decodeRequestJSON(request.Body, &empty); err != nil {
			_ = WriteError(writer, http.StatusBadRequest, CodeConfigurationInvalid, RequestIDFromContext(request.Context()))
			return
		}
	}
	result, err := r.config.Rollback(request.Context())
	if err != nil {
		writeConfigServiceError(writer, request, err)
		return
	}
	_ = WriteJSON(writer, http.StatusOK, mutationResponse(result))
}

func currentResponse(result configservice.CurrentResult) ConfigCurrentResponse {
	response := ConfigCurrentResponse{State: string(result.State), Revision: result.Revision.String(), Generation: result.Generation, Status: string(result.Status)}
	if !result.ActivatedAt.IsZero() {
		activatedAt := result.ActivatedAt
		response.ActivatedAt = &activatedAt
	}
	return response
}

func mutationResponse(result configservice.CurrentResult) ConfigMutationResponse {
	return ConfigMutationResponse{State: string(result.State), Revision: result.Revision.String(), Generation: result.Generation, Status: string(result.Status), ActivatedAt: result.ActivatedAt}
}

func decodeRequestJSON(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request contains trailing JSON")
	}
	return nil
}

func writeConfigServiceError(writer http.ResponseWriter, request *http.Request, err error) {
	requestID := RequestIDFromContext(request.Context())
	code, _ := configservice.CodeOf(err)
	switch code {
	case configservice.CodeConfigurationInvalid:
		_ = WriteError(writer, http.StatusBadRequest, CodeConfigurationInvalid, requestID)
	case configservice.CodeConfigurationTooLarge:
		_ = WriteError(writer, http.StatusRequestEntityTooLarge, CodeConfigurationTooLarge, requestID)
	case configservice.CodeRevisionMismatch:
		_ = WriteError(writer, http.StatusConflict, CodeRevisionMismatch, requestID)
	case configservice.CodeGenerationConflict:
		_ = WriteError(writer, http.StatusConflict, CodeGenerationConflict, requestID)
	case configservice.CodeNoConfiguration:
		_ = WriteError(writer, http.StatusNotFound, CodeNoConfiguration, requestID)
	case configservice.CodeNoPreviousRevision:
		_ = WriteError(writer, http.StatusNotFound, CodeNoPreviousRevision, requestID)
	case configservice.CodeCancelled:
		_ = WriteError(writer, http.StatusRequestTimeout, CodeRequestTimeout, requestID)
	default:
		_ = WriteError(writer, http.StatusServiceUnavailable, CodeConfigurationUnavailable, requestID)
	}
}
