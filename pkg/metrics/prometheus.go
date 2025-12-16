package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PrometheusMetrics provides Prometheus-compatible metrics
type PrometheusMetrics struct {
	// HTTP metrics
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPRequestSize     *prometheus.HistogramVec
	HTTPResponseSize    *prometheus.HistogramVec

	// AI metrics
	AIRequestsTotal    *prometheus.CounterVec
	AIResponseDuration *prometheus.HistogramVec
	AITokenUsage       *prometheus.CounterVec

	// Bot metrics
	BotMessagesTotal *prometheus.CounterVec
	BotCommandsTotal *prometheus.CounterVec

	// Social media metrics
	SocialPostsTotal   *prometheus.CounterVec
	SocialPostDuration *prometheus.HistogramVec
	SocialPostErrors   *prometheus.CounterVec

	// Job queue metrics
	JobQueueSize      *prometheus.GaugeVec
	JobProcessedTotal *prometheus.CounterVec
	JobDuration       *prometheus.HistogramVec
	JobErrors         *prometheus.CounterVec

	// Database metrics
	DBQueriesTotal      *prometheus.CounterVec
	DBQueryDuration     *prometheus.HistogramVec
	DBActiveConnections prometheus.Gauge

	// Cache metrics
	CacheHits   *prometheus.CounterVec
	CacheMisses *prometheus.CounterVec
	CacheSize   *prometheus.GaugeVec

	// Webhook metrics
	WebhookReceivedTotal  *prometheus.CounterVec
	WebhookProcessedTotal *prometheus.CounterVec
	WebhookErrors         *prometheus.CounterVec

	// Scheduler metrics
	SchedulerPostsProcessed *prometheus.CounterVec
	SchedulerPostsFailed    *prometheus.CounterVec
	SchedulerQueueSize      prometheus.Gauge

	// Error metrics
	ErrorsTotal *prometheus.CounterVec
}

// NewPrometheusMetrics creates a new Prometheus metrics instance
func NewPrometheusMetrics() *PrometheusMetrics {
	return &PrometheusMetrics{
		// HTTP metrics
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "endpoint"},
		),
		HTTPRequestSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_size_bytes",
				Help:    "HTTP request size in bytes",
				Buckets: []float64{100, 500, 1000, 5000, 10000, 50000, 100000},
			},
			[]string{"method", "endpoint"},
		),
		HTTPResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: []float64{100, 500, 1000, 5000, 10000, 50000, 100000},
			},
			[]string{"method", "endpoint"},
		),

		// AI metrics
		AIRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ai_requests_total",
				Help: "Total number of AI requests",
			},
			[]string{"type", "tenant_id"},
		),
		AIResponseDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ai_response_duration_seconds",
				Help:    "AI response duration in seconds",
				Buckets: []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
			},
			[]string{"type"},
		),
		AITokenUsage: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ai_tokens_used_total",
				Help: "Total AI tokens used",
			},
			[]string{"type", "model"},
		),

		// Bot metrics
		BotMessagesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "bot_messages_total",
				Help: "Total number of bot messages",
			},
			[]string{"tenant_id", "type"},
		),
		BotCommandsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "bot_commands_total",
				Help: "Total number of bot commands",
			},
			[]string{"tenant_id", "command"},
		),

		// Social media metrics
		SocialPostsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "social_posts_total",
				Help: "Total number of social media posts",
			},
			[]string{"platform", "tenant_id", "status"},
		),
		SocialPostDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "social_post_duration_seconds",
				Help:    "Social media post duration in seconds",
				Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
			},
			[]string{"platform"},
		),
		SocialPostErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "social_post_errors_total",
				Help: "Total number of social media post errors",
			},
			[]string{"platform", "tenant_id", "error_type"},
		),

		// Job queue metrics
		JobQueueSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "job_queue_size",
				Help: "Current size of job queue",
			},
			[]string{"queue", "type"},
		),
		JobProcessedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "job_processed_total",
				Help: "Total number of jobs processed",
			},
			[]string{"queue", "type", "status"},
		),
		JobDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "job_duration_seconds",
				Help:    "Job processing duration in seconds",
				Buckets: []float64{0.1, 0.5, 1.0, 5.0, 10.0, 30.0, 60.0},
			},
			[]string{"queue", "type"},
		),
		JobErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "job_errors_total",
				Help: "Total number of job errors",
			},
			[]string{"queue", "type", "error_type"},
		),

		// Database metrics
		DBQueriesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "db_queries_total",
				Help: "Total number of database queries",
			},
			[]string{"operation", "table"},
		),
		DBQueryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "db_query_duration_seconds",
				Help:    "Database query duration in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0},
			},
			[]string{"operation", "table"},
		),
		DBActiveConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "db_active_connections",
				Help: "Number of active database connections",
			},
		),

		// Cache metrics
		CacheHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_hits_total",
				Help: "Total number of cache hits",
			},
			[]string{"cache_type"},
		),
		CacheMisses: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_misses_total",
				Help: "Total number of cache misses",
			},
			[]string{"cache_type"},
		),
		CacheSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "cache_size",
				Help: "Current cache size",
			},
			[]string{"cache_type"},
		),

		// Webhook metrics
		WebhookReceivedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "webhook_received_total",
				Help: "Total number of webhooks received",
			},
			[]string{"platform", "event_type"},
		),
		WebhookProcessedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "webhook_processed_total",
				Help: "Total number of webhooks processed",
			},
			[]string{"platform", "event_type", "status"},
		),
		WebhookErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "webhook_errors_total",
				Help: "Total number of webhook errors",
			},
			[]string{"platform", "event_type", "error_type"},
		),

		// Scheduler metrics
		SchedulerPostsProcessed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "scheduler_posts_processed_total",
				Help: "Total number of posts processed by scheduler",
			},
			[]string{"platform", "status"},
		),
		SchedulerPostsFailed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "scheduler_posts_failed_total",
				Help: "Total number of failed posts",
			},
			[]string{"platform", "error_type"},
		),
		SchedulerQueueSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "scheduler_queue_size",
				Help: "Current number of posts in scheduler queue",
			},
		),

		// Error metrics
		ErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "errors_total",
				Help: "Total number of errors",
			},
			[]string{"type", "severity"},
		),
	}
}

// RecordHTTPRequest records an HTTP request
func (m *PrometheusMetrics) RecordHTTPRequest(method, endpoint string, statusCode int, duration time.Duration, requestSize, responseSize int64) {
	status := "2xx"
	if statusCode >= 300 && statusCode < 400 {
		status = "3xx"
	} else if statusCode >= 400 && statusCode < 500 {
		status = "4xx"
	} else if statusCode >= 500 {
		status = "5xx"
	}

	m.HTTPRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
	m.HTTPRequestSize.WithLabelValues(method, endpoint).Observe(float64(requestSize))
	m.HTTPResponseSize.WithLabelValues(method, endpoint).Observe(float64(responseSize))
}

// RecordAIRequest records an AI request
func (m *PrometheusMetrics) RecordAIRequest(requestType, tenantID string, duration time.Duration, tokens int64, model string) {
	m.AIRequestsTotal.WithLabelValues(requestType, tenantID).Inc()
	m.AIResponseDuration.WithLabelValues(requestType).Observe(duration.Seconds())
	if tokens > 0 {
		m.AITokenUsage.WithLabelValues(requestType, model).Add(float64(tokens))
	}
}

// RecordSocialPost records a social media post
func (m *PrometheusMetrics) RecordSocialPost(platform, tenantID, status string, duration time.Duration) {
	m.SocialPostsTotal.WithLabelValues(platform, tenantID, status).Inc()
	m.SocialPostDuration.WithLabelValues(platform).Observe(duration.Seconds())
}

// RecordJobProcessed records a processed job
func (m *PrometheusMetrics) RecordJobProcessed(queue, jobType, status string, duration time.Duration) {
	m.JobProcessedTotal.WithLabelValues(queue, jobType, status).Inc()
	m.JobDuration.WithLabelValues(queue, jobType).Observe(duration.Seconds())
}

// RecordWebhookReceived records a received webhook
func (m *PrometheusMetrics) RecordWebhookReceived(platform, eventType string) {
	m.WebhookReceivedTotal.WithLabelValues(platform, eventType).Inc()
}

// RecordSchedulerPost records a scheduler post
func (m *PrometheusMetrics) RecordSchedulerPost(platform, status string) {
	m.SchedulerPostsProcessed.WithLabelValues(platform, status).Inc()
}
