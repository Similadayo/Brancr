package utils

import (
	"errors"
	"strings"
)

// SanitizeError sanitizes error messages for external responses
// Removes internal details like file paths, stack traces, and sensitive information
func SanitizeError(err error) string {
	if err == nil {
		return "An error occurred"
	}

	errMsg := err.Error()

	// Remove file paths (common in Go errors)
	errMsg = removeFilePaths(errMsg)

	// Remove stack traces
	errMsg = removeStackTraces(errMsg)

	// Remove sensitive patterns (passwords, tokens, keys)
	errMsg = removeSensitiveInfo(errMsg)

	// Remove internal error details
	errMsg = removeInternalDetails(errMsg)

	// If error is empty after sanitization, return generic message
	if strings.TrimSpace(errMsg) == "" {
		return "An error occurred. Please try again later."
	}

	return errMsg
}

// SanitizeErrorForLog keeps full error for logging but returns sanitized version
func SanitizeErrorForLog(err error) (sanitized string, full string) {
	if err == nil {
		return "An error occurred", ""
	}
	full = err.Error()
	sanitized = SanitizeError(err)
	return sanitized, full
}

func removeFilePaths(msg string) string {
	// Remove common file path patterns
	patterns := []string{
		"/go/src/",
		"/go/pkg/",
		"/usr/local/go/",
		"/home/",
		"/Users/",
		"C:\\",
		"internal/",
		"pkg/",
	}
	for _, pattern := range patterns {
		if idx := strings.Index(msg, pattern); idx != -1 {
			// Find the end of the path (space, newline, or end)
			end := idx + len(pattern)
			for end < len(msg) && msg[end] != ' ' && msg[end] != '\n' && msg[end] != ':' {
				end++
			}
			msg = msg[:idx] + "[path]" + msg[end:]
		}
	}
	return msg
}

func removeStackTraces(msg string) string {
	// Remove stack trace indicators
	if strings.Contains(msg, "goroutine") || strings.Contains(msg, "runtime.") {
		lines := strings.Split(msg, "\n")
		var cleanLines []string
		for _, line := range lines {
			if !strings.Contains(line, "goroutine") &&
				!strings.Contains(line, "runtime.") &&
				!strings.Contains(line, "created by") &&
				!strings.HasPrefix(strings.TrimSpace(line), "at ") {
				cleanLines = append(cleanLines, line)
			}
		}
		return strings.Join(cleanLines, "\n")
	}
	return msg
}

func removeSensitiveInfo(msg string) string {
	// Remove common sensitive patterns
	sensitivePatterns := []string{
		"password",
		"token",
		"secret",
		"key",
		"api_key",
		"access_token",
		"refresh_token",
		"authorization",
		"auth",
	}

	lowerMsg := strings.ToLower(msg)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(lowerMsg, pattern) {
			// Replace with generic message
			return "Authentication or configuration error"
		}
	}

	return msg
}

func removeInternalDetails(msg string) string {
	// Remove internal error types and details
	internalPrefixes := []string{
		"sql:",
		"pq:",
		"gorm:",
		"database",
		"connection",
		"timeout",
		"context canceled",
	}

	lowerMsg := strings.ToLower(msg)
	for _, prefix := range internalPrefixes {
		if strings.HasPrefix(lowerMsg, prefix) || strings.Contains(lowerMsg, prefix) {
			// Map to user-friendly messages
			if strings.Contains(lowerMsg, "timeout") {
				return "Request timed out. Please try again."
			}
			if strings.Contains(lowerMsg, "connection") {
				return "Connection error. Please try again later."
			}
			if strings.Contains(lowerMsg, "database") {
				return "Database error. Please try again later."
			}
			return "An error occurred. Please try again."
		}
	}

	return msg
}

// SafeError wraps an error with a safe message for external use
type SafeError struct {
	PublicMessage string
	InternalError error
}

func (e *SafeError) Error() string {
	if e.PublicMessage != "" {
		return e.PublicMessage
	}
	return SanitizeError(e.InternalError)
}

func (e *SafeError) Unwrap() error {
	return e.InternalError
}

// NewSafeError creates a SafeError with a public message and internal error
func NewSafeError(publicMsg string, internalErr error) *SafeError {
	return &SafeError{
		PublicMessage: publicMsg,
		InternalError:  internalErr,
	}
}

// WrapSafe wraps an error with a safe public message
func WrapSafe(publicMsg string, err error) error {
	if err == nil {
		return nil
	}
	return &SafeError{
		PublicMessage: publicMsg,
		InternalError: err,
	}
}

// GetPublicError extracts the public-safe error message
func GetPublicError(err error) string {
	var safeErr *SafeError
	if errors.As(err, &safeErr) {
		return safeErr.PublicMessage
	}
	return SanitizeError(err)
}

