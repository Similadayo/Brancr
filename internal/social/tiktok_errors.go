package social

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TikTokErrorType represents the type of TikTok API error
type TikTokErrorType string

const (
	ErrorTypeRateLimit  TikTokErrorType = "rate_limit"
	ErrorTypeAuth       TikTokErrorType = "authentication"
	ErrorTypeValidation TikTokErrorType = "validation"
	ErrorTypeNotFound   TikTokErrorType = "not_found"
	ErrorTypeServer     TikTokErrorType = "server_error"
	ErrorTypeNetwork    TikTokErrorType = "network"
	ErrorTypeUnknown    TikTokErrorType = "unknown"
)

// EnhancedTikTokError provides structured error information
type EnhancedTikTokError struct {
	Type        TikTokErrorType
	Code        string
	Message     string
	UserMessage string // User-friendly message
	Retryable   bool
	RetryAfter  time.Duration
	LogID       string
	Status      int
	OriginalErr error
}

func (e *EnhancedTikTokError) Error() string {
	if e.UserMessage != "" {
		return e.UserMessage
	}
	if e.Message != "" {
		return e.Message
	}
	if e.OriginalErr != nil {
		return e.OriginalErr.Error()
	}
	return fmt.Sprintf("TikTok API error: %s (code: %s)", e.Type, e.Code)
}

// IsRetryable checks if the error is retryable
func (e *EnhancedTikTokError) IsRetryable() bool {
	return e.Retryable
}

// GetRetryAfter returns the recommended retry delay
func (e *EnhancedTikTokError) GetRetryAfter() time.Duration {
	return e.RetryAfter
}

// EnhanceError converts a TikTokAPIError or other error into an EnhancedTikTokError
func EnhanceError(err error, statusCode int) *EnhancedTikTokError {
	if err == nil {
		return nil
	}

	enhanced := &EnhancedTikTokError{
		Type:        ErrorTypeUnknown,
		Status:      statusCode,
		Retryable:   false,
		RetryAfter:  0,
		OriginalErr: err,
	}

	// Handle TikTokAPIError
	if apiErr, ok := err.(*TikTokAPIError); ok {
		enhanced.Code = apiErr.Code
		enhanced.Message = apiErr.Message
		enhanced.LogID = apiErr.LogID
		enhanced.Status = apiErr.Status
		enhanced.Type = classifyErrorType(apiErr.Code, apiErr.Status)
		enhanced.Retryable = isRetryableError(apiErr.Code, apiErr.Status)
		enhanced.UserMessage = getUserFriendlyMessage(apiErr.Code, apiErr.Message)

		if enhanced.Type == ErrorTypeRateLimit {
			enhanced.RetryAfter = 1 * time.Minute // Default retry after 1 minute
		}
	}

	// Handle TikTokRateLimitError
	if rateLimitErr, ok := err.(*TikTokRateLimitError); ok {
		enhanced.Type = ErrorTypeRateLimit
		enhanced.Code = "rate_limit_exceeded"
		enhanced.Message = rateLimitErr.Message
		enhanced.Retryable = true
		enhanced.RetryAfter = rateLimitErr.RetryAfter
		enhanced.UserMessage = "TikTok API rate limit exceeded. Please try again in a few minutes."
	}

	// Handle HTTP status codes
	if statusCode > 0 {
		switch statusCode {
		case http.StatusTooManyRequests:
			enhanced.Type = ErrorTypeRateLimit
			enhanced.Code = "rate_limit_exceeded"
			enhanced.Retryable = true
			enhanced.RetryAfter = 1 * time.Minute
			enhanced.UserMessage = "TikTok API rate limit exceeded. Please try again in a few minutes."
		case http.StatusUnauthorized:
			enhanced.Type = ErrorTypeAuth
			enhanced.Code = "unauthorized"
			enhanced.Retryable = false
			enhanced.UserMessage = "TikTok authentication failed. Please reconnect your TikTok account."
		case http.StatusForbidden:
			enhanced.Type = ErrorTypeAuth
			enhanced.Code = "forbidden"
			enhanced.Retryable = false
			enhanced.UserMessage = "TikTok access denied. Please check your account permissions."
		case http.StatusNotFound:
			enhanced.Type = ErrorTypeNotFound
			enhanced.Code = "not_found"
			enhanced.Retryable = false
			enhanced.UserMessage = "The requested resource was not found on TikTok."
		case http.StatusBadRequest:
			enhanced.Type = ErrorTypeValidation
			enhanced.Code = "bad_request"
			enhanced.Retryable = false
			enhanced.UserMessage = "Invalid request to TikTok API. Please check your input."
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			enhanced.Type = ErrorTypeServer
			enhanced.Code = "server_error"
			enhanced.Retryable = true
			enhanced.RetryAfter = 30 * time.Second
			enhanced.UserMessage = "TikTok API is temporarily unavailable. Please try again later."
		}
	}

	return enhanced
}

// classifyErrorType classifies error type based on error code
func classifyErrorType(code string, status int) TikTokErrorType {
	codeLower := strings.ToLower(code)

	// Rate limit errors
	if strings.Contains(codeLower, "rate_limit") || strings.Contains(codeLower, "too_many_requests") {
		return ErrorTypeRateLimit
	}

	// Authentication errors
	if strings.Contains(codeLower, "unauthorized") || strings.Contains(codeLower, "invalid_token") ||
		strings.Contains(codeLower, "token_expired") || strings.Contains(codeLower, "authentication") {
		return ErrorTypeAuth
	}

	// Validation errors
	if strings.Contains(codeLower, "invalid") || strings.Contains(codeLower, "bad_request") ||
		strings.Contains(codeLower, "validation") {
		return ErrorTypeValidation
	}

	// Not found errors
	if strings.Contains(codeLower, "not_found") || strings.Contains(codeLower, "does_not_exist") {
		return ErrorTypeNotFound
	}

	// Server errors
	if status >= 500 {
		return ErrorTypeServer
	}

	return ErrorTypeUnknown
}

// isRetryableError determines if an error is retryable
func isRetryableError(code string, status int) bool {
	codeLower := strings.ToLower(code)

	// Rate limits are retryable
	if strings.Contains(codeLower, "rate_limit") || status == http.StatusTooManyRequests || status == 429 {
		return true
	}

	// Server errors are retryable
	if status >= 500 && status < 600 {
		return true
	}

	// Network errors are retryable
	if strings.Contains(codeLower, "timeout") || strings.Contains(codeLower, "network") {
		return true
	}

	// Authentication errors are NOT retryable
	if strings.Contains(codeLower, "unauthorized") || strings.Contains(codeLower, "invalid_token") {
		return false
	}

	// Validation errors are NOT retryable
	if strings.Contains(codeLower, "invalid") || strings.Contains(codeLower, "bad_request") {
		return false
	}

	return false
}

// getUserFriendlyMessage converts technical error messages to user-friendly ones
func getUserFriendlyMessage(code, message string) string {
	codeLower := strings.ToLower(code)
	messageLower := strings.ToLower(message)

	// Rate limit messages
	if strings.Contains(codeLower, "rate_limit") || strings.Contains(messageLower, "rate limit") {
		return "TikTok API rate limit exceeded. Please try again in a few minutes."
	}

	// Authentication messages
	if strings.Contains(codeLower, "unauthorized") || strings.Contains(codeLower, "invalid_token") {
		return "TikTok authentication failed. Please reconnect your TikTok account in settings."
	}

	// Video-specific messages
	if strings.Contains(messageLower, "video") {
		if strings.Contains(messageLower, "not found") {
			return "The video was not found. It may have been deleted or is not accessible."
		}
		if strings.Contains(messageLower, "processing") {
			return "The video is still being processed. Please wait a moment and try again."
		}
	}

	// Comment-specific messages
	if strings.Contains(messageLower, "comment") {
		if strings.Contains(messageLower, "not found") {
			return "The comment was not found. It may have been deleted."
		}
	}

	// Default: return original message if it's user-friendly, otherwise generic message
	if len(message) > 0 && len(message) < 200 {
		return message
	}

	return "An error occurred while communicating with TikTok. Please try again later."
}
