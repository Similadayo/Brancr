package core

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/similadayo/brancr/internal/admin"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/tenantapi"
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
	r.Use(middleware.CORSMiddleware(app.Config.CORSAllowedOrigins))

	// Static file serving for admin assets
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Health check endpoints
	r.Get("/health", app.HealthHandler)
	r.Get("/health/ready", app.ReadinessHandler)
	r.Get("/health/live", app.LivenessHandler)

	// Prometheus metrics endpoint
	r.Handle("/metrics", promhttp.Handler())

	// Build a limited context for admin routes (breaks circular dependency)
	adminCtx := &admin.AdminContext{
		Config:        app.Config,
		DB:            app.Services.DB,
		Redis:         app.Services.Redis,
		Logger:        app.Services.Logger,
		AdminEmail:    app.Config.AdminEmail,
		AdminPassword: app.Config.AdminPassword,
	}

	r.Mount("/admin", admin.AdminRouter(adminCtx))
	r.Mount("/bot", TenantRouter(app))

	apiHandler := tenantapi.NewHandler(app.Services.DB, app.Services.Logger, app.Config, app.Services.Redis, app.Services.WorkflowOrchestrator, app.Services.Scheduler, app.Services.AIClient, app.Services.MessageHandler, app.Services.IndustryService)
	r.Route("/api", func(api chi.Router) {
		apiHandler.RegisterRoutes(api)
	})

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

	// Respond.io webhook endpoint for incoming WhatsApp messages (OPTIONAL - only needed if using webhooks on Advanced plan)
	if app.Services.RespondIOWebhookHandler != nil {
		r.HandleFunc("/api/webhooks/respondio", app.Services.RespondIOWebhookHandler.HandleRespondIOWebhook)
		r.Get("/api/webhooks/respondio/test", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"ok","endpoint":"/api/webhooks/respondio","message":"Respond.io webhook endpoint is reachable (optional - webhooks not required for sending messages)"}`)
		})
	}

	// Gupshup webhook endpoints
	if app.Services.GupshupWebhookHandler != nil {
		r.HandleFunc("/api/webhooks/gupshup", app.Services.GupshupWebhookHandler.HandleWebhook)
		r.Get("/api/webhooks/gupshup/redirect", app.Services.GupshupWebhookHandler.HandleRedirect)
		r.Get("/api/webhooks/gupshup/test", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"ok","endpoint":"/api/webhooks/gupshup","message":"Gupshup webhook endpoint is reachable"}`)
		})
	}

	// OAuth success page (public, no auth required)
	oauthSuccessHandler := social.NewOAuthSuccessHandler()
	r.Get("/oauth/success", oauthSuccessHandler.HandleSuccess)

	if app.Services.MetaOAuthHandler != nil {
		r.Get("/api/oauth/meta/start", app.Services.MetaOAuthHandler.StartOAuth)
		r.Get("/api/oauth/meta/callback", app.Services.MetaOAuthHandler.Callback)
	}

	if app.Services.TikTokOAuthHandler != nil {
		r.Get("/api/oauth/tiktok/start", app.Services.TikTokOAuthHandler.StartOAuth)
		r.Get("/api/oauth/tiktok/callback", app.Services.TikTokOAuthHandler.Callback)
	}

	if app.Services.EmbeddedSignupHandler != nil {
		r.Get("/api/oauth/meta/whatsapp/start", app.Services.EmbeddedSignupHandler.Start)
		r.MethodFunc(http.MethodGet, "/api/oauth/meta/whatsapp/callback", app.Services.EmbeddedSignupHandler.Verify)
		r.MethodFunc(http.MethodPost, "/api/oauth/meta/whatsapp/callback", app.Services.EmbeddedSignupHandler.Callback)
	}

	return r
}
