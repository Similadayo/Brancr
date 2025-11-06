package errors

import (
	"fmt"
)

// ErrorType represents the type of error
type ErrorType string

const (
	ErrorTypeValidation   ErrorType = "validation"
	ErrorTypeNotFound     ErrorType = "not_found"
	ErrorTypeUnauthorized ErrorType = "unauthorized"
	ErrorTypeInternal     ErrorType = "internal"
	ErrorTypeExternal     ErrorType = "external"
)

// AppError represents an application error with context
type AppError struct {
	Type    ErrorType
	Message string
	Err     error
	Code    int // HTTP status code
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// NewValidationError creates a validation error
func NewValidationError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeValidation,
		Message: message,
		Err:     err,
		Code:    400,
	}
}

// NewNotFoundError creates a not found error
func NewNotFoundError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeNotFound,
		Message: message,
		Err:     err,
		Code:    404,
	}
}

// NewUnauthorizedError creates an unauthorized error
func NewUnauthorizedError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeUnauthorized,
		Message: message,
		Err:     err,
		Code:    401,
	}
}

// NewInternalError creates an internal error
func NewInternalError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeInternal,
		Message: message,
		Err:     err,
		Code:    500,
	}
}

// NewExternalError creates an external service error
func NewExternalError(message string, err error) *AppError {
	return &AppError{
		Type:    ErrorTypeExternal,
		Message: message,
		Err:     err,
		Code:    502,
	}
}

