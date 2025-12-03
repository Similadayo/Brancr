package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RateLimiter provides rate limiting functionality
type RateLimiter struct {
	rdb    *redis.Client
	logger *zap.Logger
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rdb *redis.Client, logger *zap.Logger) *RateLimiter {
	return &RateLimiter{
		rdb:    rdb,
		logger: logger,
	}
}

// RateLimitConfig defines rate limit configuration
type RateLimitConfig struct {
	RequestsPerMinute int
	RequestsPerHour   int
	BurstSize         int
}

// DefaultRateLimitConfig returns default rate limit config
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 60,
		RequestsPerHour:   1000,
		BurstSize:         10,
	}
}

// TenantRateLimit middleware limits requests per tenant
func (rl *RateLimiter) TenantRateLimit(config RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl.rdb == nil {
				// Redis not available, skip rate limiting
				next.ServeHTTP(w, r)
				return
			}

			// Get tenant ID from context
			claims, ok := TenantFromContext(r.Context())
			if !ok {
				// No tenant ID, allow request
				next.ServeHTTP(w, r)
				return
			}
			tenantID := claims.TenantID

			ctx := r.Context()
			key := fmt.Sprintf("ratelimit:tenant:%d", tenantID)

			// Check per-minute limit
			minuteKey := key + ":minute"
			currentMinute, err := rl.rdb.Incr(ctx, minuteKey).Result()
			if err == nil {
				rl.rdb.Expire(ctx, minuteKey, time.Minute)
				if currentMinute > int64(config.RequestsPerMinute) {
					rl.logger.Warn("rate limit exceeded (per minute)",
						zap.Uint("tenant_id", tenantID),
						zap.Int64("current", currentMinute),
						zap.Int("limit", config.RequestsPerMinute))
					http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
					return
				}
			}

			// Check per-hour limit
			hourKey := key + ":hour"
			currentHour, err := rl.rdb.Incr(ctx, hourKey).Result()
			if err == nil {
				rl.rdb.Expire(ctx, hourKey, time.Hour)
				if currentHour > int64(config.RequestsPerHour) {
					rl.logger.Warn("rate limit exceeded (per hour)",
						zap.Uint("tenant_id", tenantID),
						zap.Int64("current", currentHour),
						zap.Int("limit", config.RequestsPerHour))
					http.Error(w, "Hourly rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GlobalRateLimit middleware limits global requests
func (rl *RateLimiter) GlobalRateLimit(requestsPerMinute int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl.rdb == nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			key := "ratelimit:global:minute"
			current, err := rl.rdb.Incr(ctx, key).Result()
			if err == nil {
				rl.rdb.Expire(ctx, key, time.Minute)
				if current > int64(requestsPerMinute) {
					http.Error(w, "Service temporarily unavailable. Please try again later.", http.StatusServiceUnavailable)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// PlatformRateLimit limits requests to external platforms per tenant
func (rl *RateLimiter) PlatformRateLimit(platform string, requestsPerMinute int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl.rdb == nil {
				next.ServeHTTP(w, r)
				return
			}

			claims, ok := TenantFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			tenantID := claims.TenantID

			ctx := r.Context()
			key := fmt.Sprintf("ratelimit:platform:%s:tenant:%d:minute", platform, tenantID)
			current, err := rl.rdb.Incr(ctx, key).Result()
			if err == nil {
				rl.rdb.Expire(ctx, key, time.Minute)
				if current > int64(requestsPerMinute) {
					rl.logger.Warn("platform rate limit exceeded",
						zap.String("platform", platform),
						zap.Uint("tenant_id", tenantID),
						zap.Int64("current", current))
					http.Error(w, fmt.Sprintf("Rate limit exceeded for %s. Please try again later.", platform), http.StatusTooManyRequests)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

