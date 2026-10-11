package errdefs

import (
	"errors"
	"fmt"
	"net/http"
)

type ErrorCode string

const (
	CodeRetryable      ErrorCode = "retryable"
	CodeAuthentication ErrorCode = "authentication"
	CodeValidation     ErrorCode = "validation"
	CodeConflict       ErrorCode = "conflict"
	CodePermanent      ErrorCode = "permanent"
	CodeUnavailable    ErrorCode = "unavailable"
)

// TypedError represents a structured, classified domain error.
type TypedError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *TypedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *TypedError) Unwrap() error {
	return e.Cause
}

func newTyped(code ErrorCode, msg string, causes []error) error {
	var cause error
	if len(causes) > 0 {
		cause = causes[0]
	}
	return &TypedError{
		Code:    code,
		Message: msg,
		Cause:   cause,
	}
}

func Retryable(msg string, cause ...error) error {
	return newTyped(CodeRetryable, msg, cause)
}

func Authentication(msg string, cause ...error) error {
	return newTyped(CodeAuthentication, msg, cause)
}

func Validation(msg string, cause ...error) error {
	return newTyped(CodeValidation, msg, cause)
}

func Conflict(msg string, cause ...error) error {
	return newTyped(CodeConflict, msg, cause)
}

func Permanent(msg string, cause ...error) error {
	return newTyped(CodePermanent, msg, cause)
}

func Unavailable(msg string, cause ...error) error {
	return newTyped(CodeUnavailable, msg, cause)
}

func AsTypedError(err error) (*TypedError, bool) {
	if err == nil {
		return nil, false
	}
	var te *TypedError
	if errors.As(err, &te) {
		return te, true
	}
	return nil, false
}

func IsRetryable(err error) bool {
	if te, ok := AsTypedError(err); ok {
		return te.Code == CodeRetryable || te.Code == CodeUnavailable
	}
	return false
}

func IsAuthentication(err error) bool {
	if te, ok := AsTypedError(err); ok {
		return te.Code == CodeAuthentication
	}
	return false
}

func IsValidation(err error) bool {
	if te, ok := AsTypedError(err); ok {
		return te.Code == CodeValidation
	}
	return false
}

func IsConflict(err error) bool {
	if te, ok := AsTypedError(err); ok {
		return te.Code == CodeConflict
	}
	return false
}

func IsPermanent(err error) bool {
	if te, ok := AsTypedError(err); ok {
		return te.Code == CodePermanent
	}
	return false
}

func IsUnavailable(err error) bool {
	if te, ok := AsTypedError(err); ok {
		return te.Code == CodeUnavailable
	}
	return false
}

// HTTPStatusCode maps typed domain errors to standard HTTP response codes.
func HTTPStatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if te, ok := AsTypedError(err); ok {
		switch te.Code {
		case CodeValidation:
			return http.StatusBadRequest
		case CodeAuthentication:
			return http.StatusUnauthorized
		case CodeConflict:
			return http.StatusConflict
		case CodeUnavailable, CodeRetryable:
			return http.StatusServiceUnavailable
		case CodePermanent:
			return http.StatusUnprocessableEntity
		}
	}
	return http.StatusInternalServerError
}
