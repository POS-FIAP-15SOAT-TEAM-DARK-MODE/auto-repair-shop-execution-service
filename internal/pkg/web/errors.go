package web

import (
	"errors"
	"net/http"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/app"
	jsonv2 "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/pkg/json"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/task"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-execution-service/internal/work"
)

const (
	msgInternal        = "internal server error"
	msgInfraConflict   = "conflict"
	msgInfraViolation  = "invalid data"
	msgUnknownNotFound = "not found"
)

// Known domain sentinels. Their own messages are safe to show to the client.
var (
	notFoundErrors = []error{
		work.ErrWorkServiceOrderNotFound,
		task.ErrTaskNotFound,
	}
	conflictErrors = []error{
		task.ErrTaskNotActive,
		task.ErrWorksNotFinished,
		work.ErrWorkServiceOrderAlreadyCompleted,
		work.ErrWorkServiceOrderAlreadyCancelled,
		work.ErrWorkServiceOrderInvalidStatus,
	}
	// Infrastructure errors: the client only gets a generic message, the cause goes to the log.
	infraConflictErrors  = []error{app.ErrDataConflict, app.ErrInfraConflict}
	infraViolationErrors = []error{app.ErrDataViolation}
)

type errorResponse struct {
	StatusCode int      `json:"code"`
	Errors     []string `json:"errors,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// Error builds an errorResponse for the given error and decides the status code.
func Error(err error) (int, errorResponse) {
	status := getHTTPStatus(err)
	stackedErrors, errorMsg := buildErrorMessages(err, status)

	return status, errorResponse{
		StatusCode: status,
		Errors:     stackedErrors,
		Error:      errorMsg,
	}
}

// IsInfraError reports whether err comes from infrastructure (its message must not reach the client).
func IsInfraError(err error) bool {
	return containsAny(err, infraConflictErrors) || containsAny(err, infraViolationErrors)
}

// getHTTPStatus determines the HTTP status code for a given error.
func getHTTPStatus(err error) int {
	switch {
	case isBadRequestError(err), IsJoined(err):
		return http.StatusBadRequest
	case isNotFoundError(err):
		return http.StatusNotFound
	case isInternalServerError(err):
		return http.StatusInternalServerError
	case isConflictError(err):
		return http.StatusConflict
	case isUnprocessableEntityError(err):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func IsJoined(err error) bool {
	type unwrapper interface {
		Unwrap() []error
	}

	var uw unwrapper
	return errors.As(err, &uw)
}

// buildErrorMessages formats the error messages based on status code.
// 5xx never exposes the error text; infrastructure 409/422 get generic messages;
// domain errors answer the message of their own sentinel, never the wrapped cause.
func buildErrorMessages(err error, status int) ([]string, string) {
	switch status {
	case http.StatusBadRequest:
		return unwrapAll(err), ""
	case http.StatusNotFound:
		return nil, domainMessage(err, notFoundErrors, msgUnknownNotFound)
	case http.StatusConflict:
		if containsAny(err, infraConflictErrors) {
			return nil, msgInfraConflict
		}
		return nil, domainMessage(err, conflictErrors, msgInfraConflict)
	case http.StatusUnprocessableEntity:
		return nil, msgInfraViolation
	default:
		return nil, msgInternal
	}
}

// domainMessage returns the message of the first sentinel matched by err, or fallback.
func domainMessage(err error, sentinels []error, fallback string) string {
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return s.Error()
		}
	}
	return fallback
}

func containsAny(err error, sentinels []error) bool {
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

func isBadRequestError(err error) bool {
	return errors.Is(err, jsonv2.ErrJSONSyntax) ||
		errors.Is(err, jsonv2.ErrJSONType) ||
		errors.Is(err, jsonv2.ErrJSONUnexpectedEOF) ||
		errors.Is(err, jsonv2.ErrJSONEmptyBody) ||
		errors.Is(err, jsonv2.ErrWrongPayloadFormat)
}

func isInternalServerError(err error) bool {
	return errors.Is(err, jsonv2.ErrJSONUnsupportedValue) ||
		errors.Is(err, jsonv2.ErrJSONUnsupportedType) ||
		errors.Is(err, jsonv2.ErrJSONInvalidUnmarshal)
}

func isNotFoundError(err error) bool {
	return containsAny(err, notFoundErrors)
}

func isConflictError(err error) bool {
	return containsAny(err, infraConflictErrors) || containsAny(err, conflictErrors)
}

func isUnprocessableEntityError(err error) bool {
	return containsAny(err, infraViolationErrors)
}

func unwrapAll(err error) []string {
	if err == nil {
		return nil
	}

	type multiUnwrap interface {
		Unwrap() []error
	}
	if m, ok := err.(multiUnwrap); ok {
		var all []string
		for _, e := range m.Unwrap() {
			all = append(all, unwrapAll(e)...)
		}
		return all
	}
	return []string{err.Error()}
}
