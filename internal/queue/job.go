package queue

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// JobType represents different types of background jobs
type JobType string

const (
	// Post execution jobs
	JobTypePostExecution JobType = "post:execute"

	// Webhook processing jobs
	JobTypeWebhookMeta      JobType = "webhook:meta"
	JobTypeWebhookGupshup   JobType = "webhook:gupshup"
	JobTypeWebhookRespondIO JobType = "webhook:respondio"

	// Media processing jobs
	JobTypeMediaUpload  JobType = "media:upload"
	JobTypeMediaConvert JobType = "media:convert"

	// AI processing jobs
	JobTypeAIGenerateReply   JobType = "ai:generate_reply"
	JobTypeAIOptimizeCaption JobType = "ai:optimize_caption"
	// Parsing import job (file uploads for parsing/ingestion)
	JobTypeParseImport JobType = "parse:import"

	// Analytics jobs
	JobTypeAnalyticsCollect   JobType = "analytics:collect"
	JobTypeAnalyticsAggregate JobType = "analytics:aggregate"

	// Cleanup jobs
	JobTypeCleanupOldLogs      JobType = "cleanup:old_logs"
	JobTypeCleanupExpiredCache JobType = "cleanup:expired_cache"
)

// JobQueue manages background job processing
type JobQueue struct {
	client *asynq.Client
	server *asynq.Server
	mux    *asynq.ServeMux
	logger *zap.Logger
	rdb    *redis.Client
}

// NewJobQueue creates a new job queue system
func NewJobQueue(redisAddr string, logger *zap.Logger) (*JobQueue, error) {
	if redisAddr == "" {
		return nil, fmt.Errorf("redis address required for job queue")
	}

	// Parse Redis URL if it's a full URL (Railway format: redis://default:password@host:port)
	var redisOpt asynq.RedisClientOpt
	if strings.HasPrefix(redisAddr, "redis://") || strings.HasPrefix(redisAddr, "rediss://") {
		// Parse the Redis URL to extract components
		opt, err := redis.ParseURL(redisAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
		}
		// Convert redis.Options to asynq.RedisClientOpt
		redisOpt = asynq.RedisClientOpt{
			Addr:     opt.Addr,
			Password: opt.Password,
			DB:       opt.DB,
		}
		// Enable TLS for rediss:// URLs (Upstash, etc.)
		if strings.HasPrefix(redisAddr, "rediss://") {
			if opt.TLSConfig != nil {
				redisOpt.TLSConfig = opt.TLSConfig
			} else {
				// Explicit TLS config for rediss:// URLs
				redisOpt.TLSConfig = &tls.Config{
					MinVersion: tls.VersionTLS12,
				}
			}
		} else if opt.TLSConfig != nil {
			redisOpt.TLSConfig = opt.TLSConfig
		}
	} else {
		// Direct address format (host:port)
		redisOpt = asynq.RedisClientOpt{
			Addr: redisAddr,
		}
	}

	client := asynq.NewClient(redisOpt)

	// Configure server with LOW concurrency for Upstash free tier
	// High concurrency causes Redis overload on limited plans
	server := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: 5, // Reduced from 50 - Upstash free tier can't handle high concurrency
			Queues: map[string]int{
				"critical": 3, // Reduced priorities
				"default":  2,
				"low":      1,
			},
			StrictPriority: true,
			// Health check interval - detect Redis issues early
			HealthCheckInterval: 30 * time.Second,
			// Delay before retrying failed tasks
			RetryDelayFunc: func(n int, e error, t *asynq.Task) time.Duration {
				// Exponential backoff: 10s, 30s, 1min, 2min, 5min
				delays := []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}
				if n >= len(delays) {
					return 5 * time.Minute
				}
				return delays[n]
			},
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				if logger != nil {
					logger.Error("job processing failed",
						zap.String("type", task.Type()),
						zap.Error(err),
						zap.ByteString("payload", task.Payload()))
				}
			}),
		},
	)

	mux := asynq.NewServeMux()

	// Get Redis client for idempotency checks with enhanced retry settings
	var rdb *redis.Client
	if redisAddr != "" {
		// Common options for handling Railway's sleep/wake cycle
		commonOpts := func(opt *redis.Options) {
			opt.DisableIdentity = true // Disable auto-detection to avoid maint_notifications errors
			opt.MaxRetries = 5         // Retry up to 5 times (helps when Redis is waking up)
			opt.MinRetryBackoff = 500 * time.Millisecond
			opt.MaxRetryBackoff = 5 * time.Second
			opt.DialTimeout = 30 * time.Second // Allow 30s for Redis to wake up
			opt.ReadTimeout = 10 * time.Second
			opt.WriteTimeout = 10 * time.Second
			opt.PoolSize = 10
			opt.MinIdleConns = 1
			opt.PoolTimeout = 30 * time.Second
			opt.ConnMaxIdleTime = 5 * time.Minute
		}

		// Parse Redis URL if it's a full URL (Railway format)
		if strings.HasPrefix(redisAddr, "redis://") || strings.HasPrefix(redisAddr, "rediss://") {
			opt, err := redis.ParseURL(redisAddr)
			if err == nil {
				commonOpts(opt)
				// Ensure TLS is enabled for rediss:// URLs (Upstash, etc.)
				if strings.HasPrefix(redisAddr, "rediss://") && opt.TLSConfig == nil {
					opt.TLSConfig = &tls.Config{
						MinVersion: tls.VersionTLS12,
					}
				}
				rdb = redis.NewClient(opt)
			} else {
				// Fallback to direct address
				fallbackOpt := &redis.Options{Addr: redisAddr}
				commonOpts(fallbackOpt)
				rdb = redis.NewClient(fallbackOpt)
			}
		} else {
			// Direct address format (host:port)
			opt := &redis.Options{Addr: redisAddr}
			commonOpts(opt)
			rdb = redis.NewClient(opt)
		}
	}

	queue := &JobQueue{
		client: client,
		server: server,
		mux:    mux,
		logger: logger,
		rdb:    rdb,
	}

	return queue, nil
}

// Enqueue adds a job to the queue
func (q *JobQueue) Enqueue(jobType JobType, payload interface{}, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	task := asynq.NewTask(string(jobType), payloadBytes, opts...)

	info, err := q.client.Enqueue(task)
	if err != nil {
		return nil, fmt.Errorf("enqueue task: %w", err)
	}

	if q.logger != nil {
		q.logger.Info("job enqueued",
			zap.String("type", string(jobType)),
			zap.String("id", info.ID),
			zap.String("queue", info.Queue))
	}

	return info, nil
}

// EnqueueCritical enqueues a high-priority job
func (q *JobQueue) EnqueueCritical(jobType JobType, payload interface{}) (*asynq.TaskInfo, error) {
	return q.Enqueue(jobType, payload, asynq.Queue("critical"))
}

// EnqueueLow enqueues a low-priority job
func (q *JobQueue) EnqueueLow(jobType JobType, payload interface{}) (*asynq.TaskInfo, error) {
	return q.Enqueue(jobType, payload, asynq.Queue("low"))
}

// EnqueueWithDelay enqueues a job with a delay
func (q *JobQueue) EnqueueWithDelay(jobType JobType, payload interface{}, delay time.Duration) (*asynq.TaskInfo, error) {
	return q.Enqueue(jobType, payload, asynq.ProcessIn(delay))
}

// RegisterHandler registers a handler for a job type
func (q *JobQueue) RegisterHandler(jobType JobType, handler asynq.Handler) {
	q.mux.HandleFunc(string(jobType), func(ctx context.Context, task *asynq.Task) error {
		return handler.ProcessTask(ctx, task)
	})
}

// GetRedisClient returns the Redis client for idempotency checks
func (q *JobQueue) GetRedisClient() *redis.Client {
	return q.rdb
}

// Start starts the job queue server
func (q *JobQueue) Start() error {
	if q.logger != nil {
		q.logger.Info("starting job queue server")
	}
	return q.server.Run(q.mux)
}

// Shutdown gracefully shuts down the job queue
func (q *JobQueue) Shutdown() {
	if q.logger != nil {
		q.logger.Info("shutting down job queue")
	}
	q.server.Shutdown()
	q.client.Close()
}

// Client returns the underlying asynq client
func (q *JobQueue) Client() *asynq.Client {
	return q.client
}

// Server returns the underlying asynq server
func (q *JobQueue) Server() *asynq.Server {
	return q.server
}
