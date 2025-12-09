package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRateLimiter_GlobalRateLimit(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rl := NewRateLimiter(nil, logger) // No Redis, use in-memory

	handler := rl.GlobalRateLimit(2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should pass
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Second request should pass
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req)
	if w2.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w2.Code)
	}

	// Third request should be rate limited
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req)
	if w3.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w3.Code)
	}
}

func TestRateLimiter_TenantRateLimit_NoTenant(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rl := NewRateLimiter(nil, logger)

	config := DefaultRateLimitConfig()
	handler := rl.TenantRateLimit(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request without tenant context should be allowed (no rate limiting)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 when no tenant context, got %d", w.Code)
	}
}

func TestRateLimiter_MemoryCleanup(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rl := NewRateLimiter(nil, logger)

	key := "test:key"
	// Add entry that should expire
	rl.memoryStore.Store(key, &memoryEntry{
		count:     1,
		expiresAt: time.Now().Add(-time.Minute), // Already expired
	})

	// Cleanup should remove expired entry
	rl.cleanupMemoryStore()

	_, exists := rl.memoryStore.Load(key)
	if exists {
		t.Error("Expected expired entry to be cleaned up")
	}
}

