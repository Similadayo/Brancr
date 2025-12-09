package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// CacheConfig defines caching configuration
type CacheConfig struct {
	TTL           time.Duration
	KeyPrefix     string
	VaryHeaders   []string // Headers that affect cache key
	SkipCache     func(r *http.Request) bool // Skip caching for specific requests
}

// DefaultCacheConfig returns default cache configuration
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		TTL:         5 * time.Minute,
		KeyPrefix:   "cache:http:",
		VaryHeaders: []string{"Authorization"},
		SkipCache: func(r *http.Request) bool {
			// Skip caching for POST, PUT, DELETE, PATCH
			return r.Method != http.MethodGet && r.Method != http.MethodHead
		},
	}
}

// ResponseCache middleware caches HTTP responses
func ResponseCache(rdb *redis.Client, logger *zap.Logger, config CacheConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip caching if configured
			if config.SkipCache != nil && config.SkipCache(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Skip if Redis not available
			if rdb == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Generate cache key
			cacheKey := generateCacheKey(r, config)

			// Try to get from cache
			ctx := r.Context()
			cached, err := rdb.Get(ctx, cacheKey).Result()
			if err == nil && cached != "" {
				// Cache hit
				logger.Debug("cache hit",
					zap.String("path", r.URL.Path),
					zap.String("key", cacheKey))

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(cached))
				return
			}

			// Cache miss - wrap response writer to capture response
			cacheWriter := &cacheResponseWriter{
				ResponseWriter: w,
				body:           make([]byte, 0),
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(cacheWriter, r)

			// Only cache successful GET requests
			if cacheWriter.statusCode == http.StatusOK && len(cacheWriter.body) > 0 {
				// Store in cache
				if err := rdb.Set(ctx, cacheKey, string(cacheWriter.body), config.TTL).Err(); err != nil {
					logger.Warn("failed to cache response",
						zap.String("path", r.URL.Path),
						zap.Error(err))
				} else {
					cacheWriter.Header().Set("X-Cache", "MISS")
					logger.Debug("cached response",
						zap.String("path", r.URL.Path),
						zap.String("key", cacheKey),
						zap.Duration("ttl", config.TTL))
				}
			}
		})
	}
}

// cacheResponseWriter captures response for caching
type cacheResponseWriter struct {
	http.ResponseWriter
	body       []byte
	statusCode int
}

func (cw *cacheResponseWriter) Write(b []byte) (int, error) {
	cw.body = append(cw.body, b...)
	return cw.ResponseWriter.Write(b)
}

func (cw *cacheResponseWriter) WriteHeader(code int) {
	cw.statusCode = code
	cw.ResponseWriter.WriteHeader(code)
}

// generateCacheKey creates a unique cache key from request
func generateCacheKey(r *http.Request, config CacheConfig) string {
	// Include method, path, and query
	key := fmt.Sprintf("%s:%s:%s", r.Method, r.URL.Path, r.URL.RawQuery)

	// Include vary headers
	for _, header := range config.VaryHeaders {
		if val := r.Header.Get(header); val != "" {
			key += ":" + header + ":" + val
		}
	}

	// Hash the key to keep it reasonable length
	hash := sha256.Sum256([]byte(key))
	return config.KeyPrefix + hex.EncodeToString(hash[:])
}

// NoCache middleware sets headers to prevent caching
func NoCache() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			next.ServeHTTP(w, r)
		})
	}
}

