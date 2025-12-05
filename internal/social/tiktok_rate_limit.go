package social

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// RateLimitInfo contains rate limit information from TikTok API responses
type RateLimitInfo struct {
	Limit     int           // Total requests allowed
	Remaining int           // Requests remaining
	ResetAt   time.Time     // When the rate limit resets
	RetryAfter time.Duration // How long to wait before retrying (from Retry-After header)
}

// TikTokRateLimitError represents a rate limit error from TikTok API
type TikTokRateLimitError struct {
	RateLimitInfo RateLimitInfo
	Message       string
	RetryAfter    time.Duration
}

func (e *TikTokRateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("TikTok rate limit exceeded. Retry after %v", e.RetryAfter)
	}
	return "TikTok rate limit exceeded"
}

// IsRateLimitError checks if an error is a rate limit error
func IsRateLimitError(err error) bool {
	_, ok := err.(*TikTokRateLimitError)
	return ok
}

// parseRateLimitHeaders parses rate limit headers from TikTok API response
// TikTok may use standard headers like X-RateLimit-* or Retry-After
func parseRateLimitHeaders(resp *http.Response) RateLimitInfo {
	info := RateLimitInfo{}

	// Parse X-RateLimit-Limit
	if limitStr := resp.Header.Get("X-RateLimit-Limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			info.Limit = limit
		}
	}

	// Parse X-RateLimit-Remaining
	if remainingStr := resp.Header.Get("X-RateLimit-Remaining"); remainingStr != "" {
		if remaining, err := strconv.Atoi(remainingStr); err == nil {
			info.Remaining = remaining
		}
	}

	// Parse X-RateLimit-Reset (Unix timestamp)
	if resetStr := resp.Header.Get("X-RateLimit-Reset"); resetStr != "" {
		if resetUnix, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			info.ResetAt = time.Unix(resetUnix, 0)
		}
	}

	// Parse Retry-After header (seconds or HTTP date)
	if retryAfterStr := resp.Header.Get("Retry-After"); retryAfterStr != "" {
		// Try parsing as seconds (integer)
		if seconds, err := strconv.Atoi(retryAfterStr); err == nil {
			info.RetryAfter = time.Duration(seconds) * time.Second
		} else {
			// Try parsing as HTTP date
			if t, err := time.Parse(time.RFC1123, retryAfterStr); err == nil {
				info.RetryAfter = time.Until(t)
				if info.RetryAfter < 0 {
					info.RetryAfter = 0
				}
			}
		}
	}

	return info
}

// checkRateLimitResponse checks if the response indicates a rate limit error
func checkRateLimitResponse(resp *http.Response, body []byte) error {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != 429 {
		return nil // Not a rate limit error
	}

	rateLimitInfo := parseRateLimitHeaders(resp)

	// Default retry after if not specified
	retryAfter := rateLimitInfo.RetryAfter
	if retryAfter == 0 {
		// Default to exponential backoff: 1 minute, 2 minutes, 4 minutes, etc.
		retryAfter = 1 * time.Minute
	}

	return &TikTokRateLimitError{
		RateLimitInfo: rateLimitInfo,
		Message:       "TikTok API rate limit exceeded",
		RetryAfter:    retryAfter,
	}
}

// executeWithRateLimitRetry executes an HTTP request with automatic rate limit retry
// IMPORTANT: The request body must be provided as bytes so it can be recreated on retry
// If req.Body is nil, it will be used as-is. Otherwise, the body bytes should be passed separately.
func (t *TikTokConnector) executeWithRateLimitRetry(ctx context.Context, req *http.Request, maxRetries int) (*http.Response, error) {
	// Read and store request body bytes for retries (if body exists)
	var bodyBytes []byte
	var bodyErr error
	if req.Body != nil {
		bodyBytes, bodyErr = io.ReadAll(req.Body)
		req.Body.Close()
		if bodyErr != nil {
			return nil, fmt.Errorf("failed to read request body: %w", bodyErr)
		}
	}

	var lastErr error
	var lastResp *http.Response

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Recreate request body on retry (bodies can only be read once)
		if attempt > 0 && len(bodyBytes) > 0 {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		} else if attempt == 0 && len(bodyBytes) > 0 {
			// First attempt: recreate body from bytes
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		if attempt > 0 {
			// Calculate backoff delay
			backoff := time.Duration(attempt) * time.Minute
			
			// If we have rate limit info, use the Retry-After value
			if rateLimitErr, ok := lastErr.(*TikTokRateLimitError); ok {
				backoff = rateLimitErr.RetryAfter
				if backoff > 10*time.Minute {
					backoff = 10 * time.Minute // Cap at 10 minutes
				}
			}

			t.Logger.Warn("⏳ TikTok rate limit hit, retrying after backoff",
				zap.Int("attempt", attempt),
				zap.Duration("backoff", backoff),
				zap.String("url", req.URL.String()))

			// Wait before retrying
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				// Continue to retry
			}
		}

		// Execute request
		resp, err := t.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				continue // Retry on network errors
			}
			return nil, fmt.Errorf("tiktok request failed after %d attempts: %w", attempt+1, err)
		}

		// Check for rate limit
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 429 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			
			rateLimitErr := checkRateLimitResponse(resp, body)
			if rateLimitErr != nil {
				lastErr = rateLimitErr
				lastResp = resp
				
				// Log rate limit info
				if rlErr, ok := rateLimitErr.(*TikTokRateLimitError); ok {
					t.Logger.Warn("🚫 TikTok rate limit exceeded",
						zap.Int("limit", rlErr.RateLimitInfo.Limit),
						zap.Int("remaining", rlErr.RateLimitInfo.Remaining),
						zap.Duration("retry_after", rlErr.RetryAfter),
						zap.String("url", req.URL.String()))
				}

				if attempt < maxRetries {
					continue // Retry after backoff
				}
				return nil, rateLimitErr
			}
		}

		// Success or non-rate-limit error
		return resp, nil
	}

	// All retries exhausted
	if lastResp != nil {
		lastResp.Body.Close()
	}
	return nil, fmt.Errorf("tiktok rate limit retries exhausted: %w", lastErr)
}

