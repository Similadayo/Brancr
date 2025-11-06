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

	checks := make(map[string]string)
	allHealthy := true

	// Check database connection
	if err := a.Services.DB.Exec("SELECT 1").Error; err != nil {
		checks["database"] = "unhealthy: " + err.Error()
		allHealthy = false
	} else {
		checks["database"] = "healthy"
	}

	// Check Redis connection (if configured)
	if a.Services.Redis != nil {
		ctx := r.Context()
		if err := a.Services.Redis.Ping(ctx).Err(); err != nil {
			checks["redis"] = "unhealthy: " + err.Error()
			allHealthy = false
		} else {
			checks["redis"] = "healthy"
		}
	}

	status := HealthStatus{
		Status:    "ready",
		Timestamp: time.Now(),
		Checks:    checks,
	}

	if !allHealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		status.Status = "not_ready"
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(status)
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
