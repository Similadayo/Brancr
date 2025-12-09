package core

import (
	"encoding/json"
	"net/http"
	"time"
)

// HealthStatus represents the health check response
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// HealthHandler handles basic health checks
func (a *App) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	status := HealthStatus{
		Status:    "ok",
		Timestamp: time.Now(),
	}

	json.NewEncoder(w).Encode(status)
}

// ReadinessHandler checks if the service is ready to accept traffic
func (a *App) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	checks := make(map[string]interface{})
	allHealthy := true

	// Check database connection
	dbStart := time.Now()
	if err := a.Services.DB.Exec("SELECT 1").Error; err != nil {
		checks["database"] = map[string]interface{}{
			"status":  "unhealthy",
			"error":   err.Error(),
			"latency": time.Since(dbStart).Milliseconds(),
		}
		allHealthy = false
	} else {
		checks["database"] = map[string]interface{}{
			"status":  "healthy",
			"latency": time.Since(dbStart).Milliseconds(),
		}
	}

	// Check Redis connection (if configured)
	if a.Services.Redis != nil {
		ctx := r.Context()
		redisStart := time.Now()
		if err := a.Services.Redis.Ping(ctx).Err(); err != nil {
			checks["redis"] = map[string]interface{}{
				"status":  "unhealthy",
				"error":   err.Error(),
				"latency": time.Since(redisStart).Milliseconds(),
			}
			allHealthy = false
		} else {
			checks["redis"] = map[string]interface{}{
				"status":  "healthy",
				"latency": time.Since(redisStart).Milliseconds(),
			}
		}
	} else {
		checks["redis"] = map[string]interface{}{
			"status": "not_configured",
		}
	}

	// Check database connection pool
	if sqlDB, err := a.Services.DB.DB(); err == nil {
		stats := sqlDB.Stats()
		checks["database_pool"] = map[string]interface{}{
			"open_connections":     stats.OpenConnections,
			"in_use":               stats.InUse,
			"idle":                 stats.Idle,
			"wait_count":           stats.WaitCount,
			"wait_duration_ms":     stats.WaitDuration.Milliseconds(),
			"max_idle_closed":      stats.MaxIdleClosed,
			"max_idle_time_closed": stats.MaxIdleTimeClosed,
		}
	}

	if !allHealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// Return detailed checks in response
	statusText := "ready"
	if !allHealthy {
		statusText = "not_ready"
	}

	response := map[string]interface{}{
		"status":    statusText,
		"timestamp": time.Now(),
		"checks":    checks,
	}

	json.NewEncoder(w).Encode(response)
}

// LivenessHandler checks if the service is alive
func (a *App) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	status := HealthStatus{
		Status:    "alive",
		Timestamp: time.Now(),
	}

	json.NewEncoder(w).Encode(status)
}
