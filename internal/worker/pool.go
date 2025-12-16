package worker

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Job represents a unit of work
type Job interface {
	Execute(ctx context.Context) error
}

// JobFunc is a function that implements Job interface
type JobFunc func(ctx context.Context) error

func (f JobFunc) Execute(ctx context.Context) error {
	return f(ctx)
}

// Pool manages a pool of workers
type Pool struct {
	size       int
	queue      chan Job
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *zap.Logger
	activeJobs int64
	mu         sync.RWMutex
	queueDepth int64
}

// NewPool creates a new worker pool
func NewPool(size int, queueSize int, logger *zap.Logger) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		size:   size,
		queue:  make(chan Job, queueSize),
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
	}
}

// Start starts the worker pool
func (p *Pool) Start() {
	for i := 0; i < p.size; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	p.logger.Info("worker pool started",
		zap.Int("size", p.size),
		zap.Int("queue_size", cap(p.queue)))
}

// worker is the worker goroutine
func (p *Pool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.queue:
			if !ok {
				return
			}

			p.mu.Lock()
			p.activeJobs++
			p.queueDepth--
			p.mu.Unlock()

			// Execute job with timeout
			jobCtx, cancel := context.WithTimeout(p.ctx, 5*time.Minute)
			start := time.Now()
			err := job.Execute(jobCtx)
			duration := time.Since(start)
			cancel()

			p.mu.Lock()
			p.activeJobs--
			p.mu.Unlock()

			if err != nil {
				p.logger.Error("job execution failed",
					zap.Int("worker_id", id),
					zap.Duration("duration", duration),
					zap.Error(err))
			} else {
				p.logger.Debug("job completed",
					zap.Int("worker_id", id),
					zap.Duration("duration", duration))
			}
		}
	}
}

// Submit submits a job to the pool
func (p *Pool) Submit(job Job) error {
	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	case p.queue <- job:
		p.mu.Lock()
		p.queueDepth++
		p.mu.Unlock()
		return nil
	default:
		// Queue is full
		p.mu.RLock()
		depth := p.queueDepth
		p.mu.RUnlock()
		p.logger.Warn("worker pool queue full, rejecting job",
			zap.Int64("queue_depth", depth),
			zap.Int("queue_capacity", cap(p.queue)))
		return ErrQueueFull
	}
}

// SubmitFunc submits a function as a job
func (p *Pool) SubmitFunc(fn func(ctx context.Context) error) error {
	return p.Submit(JobFunc(fn))
}

// Shutdown gracefully shuts down the pool
func (p *Pool) Shutdown(timeout time.Duration) error {
	p.logger.Info("shutting down worker pool")
	close(p.queue)
	p.cancel()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("worker pool shut down gracefully")
		return nil
	case <-time.After(timeout):
		p.logger.Warn("worker pool shutdown timeout")
		return ErrShutdownTimeout
	}
}

// Stats contains pool statistics
type Stats struct {
	Size       int
	ActiveJobs int64
	QueueDepth int64
	QueueSize  int64
}

// Stats returns pool statistics
func (p *Pool) Stats() Stats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return Stats{
		Size:       p.size,
		ActiveJobs: p.activeJobs,
		QueueDepth: p.queueDepth,
		QueueSize:  int64(cap(p.queue)),
	}
}

var (
	ErrQueueFull       = &PoolError{Message: "worker pool queue is full"}
	ErrShutdownTimeout = &PoolError{Message: "worker pool shutdown timeout"}
)

// PoolError represents a pool error
type PoolError struct {
	Message string
}

func (e *PoolError) Error() string {
	return e.Message
}
