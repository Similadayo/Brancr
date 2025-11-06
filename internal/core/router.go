package core

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/admin"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/pkg/metrics"
)

func SetupRouter(app *App) http.Handler {
	r := chi.NewRouter()

	// Initialize metrics
	appMetrics := metrics.NewMetrics()
	app.Services.Metrics = appMetrics

	// Global middleware
	r.Use(middleware.RequestIDMiddleware())
	r.Use(middleware.RecoveryMiddleware(app.Services.Logger))
	r.Use(middleware.LoggingMiddleware(app.Services.Logger))
	r.Use(middleware.MetricsMiddleware(appMetrics))

	// Static file serving for admin assets
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Health check endpoints
	r.Get("/health", app.HealthHandler)
	r.Get("/health/ready", app.ReadinessHandler)
	r.Get("/health/live", app.LivenessHandler)

	// Build a limited context for admin routes (breaks circular dependency)
	adminCtx := &admin.AdminContext{
		DB:            app.Services.DB,
		Redis:         app.Services.Redis,
		Logger:        app.Services.Logger,
		AdminEmail:    app.Config.AdminEmail,
		AdminPassword: app.Config.AdminPassword,
	}

	r.Mount("/admin", admin.AdminRouter(adminCtx))
	r.Mount("/bot", TenantRouter(app))

	// Meta webhook endpoint (single global endpoint for all tenants)
	// This handles Instagram, Facebook, and WhatsApp webhooks
	if app.Services.MetaWebhookHandler != nil {
		r.HandleFunc("/api/webhooks/meta", app.Services.MetaWebhookHandler.HandleMetaWebhook)
		// Also add a test endpoint to verify server is reachable
		r.Get("/api/webhooks/meta/test", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"ok","endpoint":"/api/webhooks/meta","message":"Meta webhook endpoint is reachable"}`)
		})
	}

	return r
}
