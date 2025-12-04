package queue

import (
	"context"
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
	JobTypeWebhookMeta        JobType = "webhook:meta"
	JobTypeWebhookGupshup     JobType = "webhook:gupshup"
	JobTypeWebhookRespondIO   JobType = "webhook:respondio"
	
	// Media processing jobs
	JobTypeMediaUpload        JobType = "media:upload"
	JobTypeMediaConvert       JobType = "media:convert"
	
	// AI processing jobs
	JobTypeAIGenerateReply    JobType = "ai:generate_reply"
	JobTypeAIOptimizeCaption  JobType = "ai:optimize_caption"
	
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
		if opt.TLSConfig != nil {
			redisOpt.TLSConfig = opt.TLSConfig
		}
	} else {
		// Direct address format (host:port)
		redisOpt = asynq.RedisClientOpt{
			Addr: redisAddr,
		}
	}

	client := asynq.NewClient(redisOpt)
	
	// Configure server with concurrency limits
	server := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: 50, // Process 50 jobs concurrently
			Queues: map[string]int{
				"critical": 10, // High priority queue
				"default":  6,  // Normal priority queue
				"low":      2,  // Low priority queue
			},
			StrictPriority: true, // Process critical jobs first
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

	// Get Redis client for idempotency checks
	var rdb *redis.Client
	if redisAddr != "" {
		// Parse Redis URL if it's a full URL (Railway format)
		if strings.HasPrefix(redisAddr, "redis://") || strings.HasPrefix(redisAddr, "rediss://") {
			opt, err := redis.ParseURL(redisAddr)
			if err == nil {
				rdb = redis.NewClient(opt)
			} else {
				// Fallback to direct address
				rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
			}
		} else {
			// Direct address format (host:port)
			rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
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

