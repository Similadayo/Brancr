package tenantapi

import (
	"fmt"
	"net/http"

	"github.com/similadayo/brancr/pkg/errors"
	"go.uber.org/zap"
)

// writeError writes a standardized error response using existing writeJSON helper
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error, statusCode int) {
	// Log the error with context
	h.Logger.Error("request error",
		zap.Error(err),
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method),
		zap.Int("status_code", statusCode))

	// Check if it's an AppError for structured response
	if appErr, ok := err.(*errors.AppError); ok {
		writeJSON(w, statusCode, map[string]interface{}{
			"error":   string(appErr.Type),
			"message": appErr.Message,
			"code":    appErr.Code,
		})
		return
	}

	// Generic error response
	writeJSON(w, statusCode, map[string]interface{}{
		"error":   "internal_error",
		"message": err.Error(),
		"code":    statusCode,
	})
}

// writeValidationError writes a validation error response
func (h *Handler) writeValidationError(w http.ResponseWriter, r *http.Request, message string, details map[string]string) {
	h.Logger.Warn("validation error",
		zap.String("path", r.URL.Path),
		zap.String("method", r.Method),
		zap.String("message", message),
		zap.Any("details", details))

	response := map[string]interface{}{
		"error":   "validation_error",
		"message": message,
		"code":    400,
	}

	if len(details) > 0 {
		response["details"] = details
	}

	writeJSON(w, http.StatusBadRequest, response)
}

// writeNotFoundError writes a not found error response
func (h *Handler) writeNotFoundError(w http.ResponseWriter, r *http.Request, resource string) {
	h.writeError(w, r, errors.NewNotFoundError(
		fmt.Sprintf("%s not found", resource),
		nil,
	), http.StatusNotFound)
}

// writeUnauthorizedError writes an unauthorized error response
func (h *Handler) writeUnauthorizedError(w http.ResponseWriter, r *http.Request, message string) {
	if message == "" {
		message = "unauthorized"
	}
	h.writeError(w, r, errors.NewUnauthorizedError(message, nil), http.StatusUnauthorized)
}

// writeInternalError writes an internal server error response
func (h *Handler) writeInternalError(w http.ResponseWriter, r *http.Request, err error) {
	h.writeError(w, r, errors.NewInternalError("internal server error", err), http.StatusInternalServerError)
}

