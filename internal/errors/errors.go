package errors

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/etkecc/go-apm"
)

// Error is a struct for a Docker-compatible error
type Error struct {
	HTTPCode int    `json:"-"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Detail   any    `json:"detail"`
}

// Error returns the error message
func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}

// Response is a struct for Docker error response
type Response struct {
	Errors []*Error `json:"errors"`
}

// WriteTo writes the error response to the http.ResponseWriter
func (e *Response) WriteTo(ctx context.Context, w http.ResponseWriter) {
	if len(e.Errors) == 0 {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Errors[0].HTTPCode)
	if err := json.NewEncoder(w).Encode(e); err != nil {
		apm.Log(ctx).Error().Err(err).Msg("failed to write error response")
	}
}

// NewError creates a new DockerError
func NewError(code, message string, details ...any) *Error {
	err := &Error{
		Code:    code,
		Message: message,
	}
	if len(details) > 0 {
		err.Detail = details[0]
	}
	return err
}

// NewResponse creates a new DockerErrorResponse
func NewResponse(httpCode int, details ...any) *Response {
	message := http.StatusText(httpCode)
	code := strings.ToUpper(strings.ReplaceAll(message, " ", "_"))
	err := NewError(code, message, details...)
	err.HTTPCode = httpCode

	return &Response{
		Errors: []*Error{err},
	}
}
